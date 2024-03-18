package raft

import (
	"math/rand"
	"time"
)

// 选举逻辑
const (
	electionTimeoutMin time.Duration = 250 * time.Millisecond
	electionTimeoutMax time.Duration = 400 * time.Millisecond
)

type RequestVoteArgs struct {
	// Your data here (PartA, PartB).
	Term        int
	CandidateId int
}

// example RequestVote RPC reply structure.
// field names must start with capital letters!
type RequestVoteReply struct {
	// Your data here (PartA).
	Term        int
	VoteGranted bool
}

func (rf *Raft) resetElectionTimeoutLocked() {
	// rf.currentTerm++
	rf.electionStart = time.Now()
	randRange := int64(electionTimeoutMax - electionTimeoutMin)
	rf.electionTimeout = electionTimeoutMin + time.Duration(rand.Int63()%randRange)
}

func (rf *Raft) isElectionTimeoutLocked() bool {
	return time.Since(rf.electionStart) > rf.electionTimeout

}

func (rf *Raft) startElection(term int) bool {
	votes := 0
	askVoteFromPeer := func(peer int, args *RequestVoteArgs) {
		reply := &RequestVoteReply{}
		ok := rf.sendRequestVote(peer, args, reply)

		rf.mu.Lock()
		defer rf.mu.Unlock()

		if !ok {
			LOG(rf.me, rf.currentTerm, DDebug, "Ask vote from %d, Lost or error", peer)
			return
		}

		//对齐任期
		if reply.Term > rf.currentTerm {
			rf.becomeFollowerLocked(reply.Term)
			return
		}

		if reply.VoteGranted {
			votes++
		}

		if votes > len(rf.peers)/2 {
			rf.becomeLeaderLocked()
			go rf.replicationTicker(term)
		}
	}
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.contextLostLocked(Candidate, term) {
		return false
	}

	for peer := 0; peer < len(rf.peers); peer++ {
		if peer == rf.me {
			votes++
			continue
		}
		args := &RequestVoteArgs{
			Term:        term,
			CandidateId: rf.me,
		}

		go askVoteFromPeer(peer, args) //非临界区调用
	}
	return true
}

// example RequestVote RPC handler.
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (PartA, PartB).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	reply.Term = rf.currentTerm
	if rf.currentTerm > args.Term {
		LOG(rf.me, rf.currentTerm, DVote, "-> S%d, Reject vote, higher term, T%d>T%d", args.CandidateId, rf.currentTerm, args.Term)
		reply.VoteGranted = false
		return
	}

	if rf.currentTerm < args.Term {
		rf.becomeFollowerLocked(args.Term)
	}

	if rf.votedFor != -1 && rf.votedFor != args.CandidateId { //
		LOG(rf.me, rf.currentTerm, DVote, "-> S%d, Reject, Already voted S%d", args.CandidateId, rf.votedFor)
		reply.VoteGranted = false
		return
	}

	reply.VoteGranted = true
	rf.votedFor = args.CandidateId
	rf.resetElectionTimeoutLocked()
	LOG(rf.me, rf.currentTerm, DVote, "-> S%d", args.CandidateId)
}

func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

func (rf *Raft) electionTicker() { //Follower开始选举的ticker
	for !rf.killed() {
		// Your code here (PartA)
		// Check if a leader election should be started.
		rf.mu.Lock()
		if rf.role != Leader && rf.isElectionTimeoutLocked() {
			rf.becomeCandidateLocked()
			go rf.startElection(rf.currentTerm)
		}
		rf.mu.Unlock()
		// pause for a random amount of time between 50 and 350
		// milliseconds.
		ms := 50 + (rand.Int63() % 300)
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
}