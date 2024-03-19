package raft

import (
	"time"
)

const (
	replicateInterval time.Duration = 200 * time.Millisecond
)

type LogEntry struct {
	Term         int
	CommandValid bool
	Command      interface{}
}

type AppendEntriesArgs struct {
	Term     int
	LeaderId int

	PrevLogIndex int
	PrevLogTerm  int
	Entries      []LogEntry
	LeaderCommit int
}
type AppendEntriesReply struct {
	Term    int
	Success bool
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	//DEBUG
	LOG(rf.me, rf.currentTerm, DDebug, "<- S%d, Receive log, Prev=[%d]T%d, Len()=%d", args.LeaderId, args.PrevLogIndex, args.PrevLogTerm, len(args.Entries))

	reply.Term = rf.currentTerm
	if rf.currentTerm > args.Term {
		LOG(rf.me, rf.currentTerm, DLog2, "<- S%d, Reject log", args.LeaderId)
		reply.Success = false
		return
	}

	if args.Term >= rf.currentTerm {
		rf.becomeFollowerLocked(args.Term)
	}

	if args.PrevLogIndex >= len(rf.log) {
		LOG(rf.me, rf.currentTerm, DLog2, "<- S%d, Reject Log, Follower log too short, Len:%d <= Prev:%d", args.LeaderId, len(rf.log), args.PrevLogIndex)
		reply.Success = false
		return
	}

	if rf.log[args.PrevLogIndex].Term != args.PrevLogTerm {
		LOG(rf.me, rf.currentTerm, DLog2, "<- S%d, Reject Log, Prev log not match, [%d]: T%d != T%d", args.LeaderId, args.PrevLogIndex, rf.log[args.PrevLogIndex].Term, args.PrevLogTerm)
		reply.Success = false
		return
	}

	rf.log = append(rf.log[:args.PrevLogIndex+1], args.Entries...)
	LOG(rf.me, rf.currentTerm, DLog2, "Follower append logs: (%d, %d]", args.PrevLogIndex, args.PrevLogIndex+len(args.Entries))

	reply.Success = true
	rf.resetElectionTimeoutLocked()
}

func (rf *Raft) startReplication(term int) bool { //心跳/日志同步
	replicateToPeer := func(peer int, args *AppendEntriesArgs) {
		reply := &AppendEntriesReply{}
		ok := rf.sendAppendEntries(peer, args, reply)
		if !ok {
			LOG(rf.me, rf.currentTerm, DLog, "-> S%d, Lost or crashed", peer)
			return
		}
		rf.mu.Lock()
		defer rf.mu.Unlock()

		if reply.Term > rf.currentTerm {
			rf.becomeFollowerLocked(reply.Term)
			return
		}

		if !reply.Success {
			//日志一致性检查失败:定位第一个合法的位置?
			idx := rf.nextIndex[peer] - 1
			term := rf.log[idx].Term
			for idx>0&&rf.log[idx].Term==term{
				idx--
			}
			rf.nextIndex[peer] = idx+1
			//LOG TODO
			return
		}

		rf.matchIndex[peer] = args.PrevLogIndex + len(args.Entries)
		rf.nextIndex[peer] = rf.matchIndex[peer] + 1
		//TODO : commit ID

	}

	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.contextLostLocked(Leader, term) {
		LOG(rf.me, rf.currentTerm, DLeader, "Leader[T%d] -> %s[T%d]", term, rf.role, rf.currentTerm)
		return false
	}

	for peer := 0; peer < len(rf.peers); peer++ {
		if peer == rf.me {
			rf.matchIndex[peer] = len(rf.log) - 1
			rf.nextIndex[peer] = len(rf.log)
			continue
		}

		prevIdx := rf.nextIndex[peer] - 1 // 最后一条日志
		prevTerm := rf.log[prevIdx].Term



		args := &AppendEntriesArgs{
			Term:     term,
			LeaderId: rf.me,
			PrevLogIndex:  prevIdx,
			PrevLogTerm: prevTerm,
			Entries: append([]LogEntry(nil),rf.log[prevIdx+1:]...),
			LeaderCommit: rf.commitIndex,
		}

		go replicateToPeer(peer, args)
	}
	return true
}

// Tick only span for one term.
// That is, if term changed, the ticker will end.

func (rf *Raft) replicationTicker(term int) { // 日志同步Ticker的生命周期为一个term
	for !rf.killed() {
		contextRemained := rf.startReplication(term)
		if !contextRemained {
			break
		}

		time.Sleep(replicateInterval)
	}
}

func (rf *Raft) isMoreUpToDateLocked(candidateIndex int, candidateTerm int) bool {
	n := len(rf.log)
	lastLogTerm, lastLogIndex := rf.log[n-1].Term, n-1

	if lastLogTerm != candidateTerm {
		return lastLogTerm > candidateTerm
	}

	return lastLogIndex > candidateIndex
}
