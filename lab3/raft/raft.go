package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	//	"bytes"
	"sync"
	"sync/atomic"

	//	"6.824/labgob"
	"6.824/labrpc"

	"rand"
	"time"
)

// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int
}

// Struct for each log entry
type Log struct {
	command interface{} // command for state machine
	term    int         // term when entry was received by leader (first index is 1)
}

const roleLeader = 0
const roleFollower = 1
const roleCandidate = 2

// A Go object implementing a single Raft peer.
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()

	// Your data here (3A, 3B, 3C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.

	myRole          int           // 0, 1, or 2
	electionTimeout time.Duration // randomized election timeout
	lastHeartbeat   time.Time     // timestamp of last heartbeat received
	heartbeatInit   bool          // If I'm leader, whether I already have background thread sending heartbeats

	// Persistent state on all servers:
	// (updated on stable storage before responding to RPCs)
	currentTerm int   // latest term server has seen (initialized to 0 on first boot, increases monotonically)
	votedFor    int   // candidateId that received vote in current term (or -1 if none)
	log         []Log // log entries

	// Volatile state on all servers:
	commitIndex uint64 // index of highest log entry known to be committed (initialized to 0, increases monotonically)
	lastApplied uint64 // index of highest log entry applied to state machine (initialized to 0, increases monotonically)

	// Volatile state on leaders:
	// (reinitialized after election)
	nextIndex  []uint64 // for each server, index of the next log entry to send to that server (initialized to leader last log index + 1)
	matchIndex []uint64 // for each server, index of highest log entry known to be replicated on server (initialized to 0, increases monotonically)
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {

	var term int
	var isleader bool

	// Your code here (3A).
	rf.mu.Lock()
	term = rf.currentTerm
	if rf.myRole == roleLeader {
		isleader = true
	} else {
		isleader = false
	}
	rf.mu.Unlock()

	return term, isleader
}

// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
func (rf *Raft) persist() {
	// Your code here (3C).
	// Example:
	// w := new(bytes.Buffer)
	// e := labgob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// data := w.Bytes()
	// rf.persister.SaveRaftState(data)
}

// restore previously persisted state.
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (3C).
	// Example:
	// r := bytes.NewBuffer(data)
	// d := labgob.NewDecoder(r)
	// var xxx
	// var yyy
	// if d.Decode(&xxx) != nil ||
	//    d.Decode(&yyy) != nil {
	//   error...
	// } else {
	//   rf.xxx = xxx
	//   rf.yyy = yyy
	// }
}

// AppendEntries RPC request structure
type AppendEntriesArgs struct {
	Term         int    // leader's term
	LeaderId     int    // so follower can redirect clients
	PrevLogIndex uint64 // index of log entry immediately preceding new ones
	PrevLogTerm  int    // term of prevLogIndex entry
	Entries      []Log  // log entries to store (empty for heartbeat; may send more than one for efficiency)
	LeaderCommit uint64 // leader’s commitIndex
}

// AppendEntries RPC reply structure
type AppendEntriesReply struct {
	Term    int  // currentTerm, for leader to update itself
	Success bool // true if follower contained entry matching prevLogIndex and prevLogTerm
}

// example RequestVote RPC arguments structure.
// field names must start with capital letters!
type RequestVoteArgs struct {
	// Your data here (3A, 3B).
	Term         int    // candidate's term
	CandidateId  int    // candidate requesting vote
	LastLogIndex uint64 // index of candidate's last log entry
	LastLogTerm  int    // term of candidate's last log entry
}

// example RequestVote RPC reply structure.
// field names must start with capital letters!
type RequestVoteReply struct {
	// Your data here (3A).
	Term        int  // currentTerm, for candidate to update itself
	VoteGranted bool // true means candidate receives vote
}

// AppendEntries RPC handler for the receiver
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// (1) Always reject request if args.term < currentTerm (5.1)
	if args.Term < rf.currentTerm {
		reply.Success = false
		reply.Term = rf.currentTerm
		return
	}

	// (2) Reset election timer as long as the AppendEntries does not have stale term number
	rf.lastHeartbeat = time.Now()

	// (3) If RPC request or response contains term T > currentTerm: set currentTerm = T,
	//     convert to follower (5.1)
	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.myRole = roleFollower
		rf.votedFor = int(args.LeaderId)
	}

	// (4) If I'm a candidate but I receive an AppendEntries from another server claiming to
	//     be the leader, and if the leader's term >= my term, then I recognize the leader
	if rf.myRole == roleCandidate && args.Term >= rf.currentTerm {
		rf.currentTerm = args.Term
		rf.myRole = roleFollower
		rf.votedFor = int(args.LeaderId)
	}

	// Reply with my updated current term
	reply.Term = rf.currentTerm

	// (5) Log consistency checK:
	//     Reply false if log doesn't contain an entry at prevLogIndex whose term matches
	//     prevLogTerm (5.3)
	if args.PrevLogIndex >= uint64(len(rf.log)) || rf.log[args.PrevLogIndex].term != args.PrevLogTerm {
		reply.Success = false
		return
	}

	// (6) Leader forces follower to duplicate its own log:
	//     If an existing entry conflicts with a new one (same index but different terms),
	//     delete the existing entry and all that follow it (5.3)
	var i int
	for i = 0; i < len(args.Entries); i++ {
		newEntry := args.Entries[i]
		index := i + int(args.PrevLogIndex)

		if index < len(rf.log) && newEntry.term != rf.log[index].term {
			// Conflict: truncate
			rf.log = rf.log[:index]
			break
		}
	}

	// (7) Append any new entries not already in the log
	rf.log = append(rf.log, args.Entries[i:]...)

	// (8) Updating commit index:
	//     If leaderCommit > commitIndex, set commitIndex = min(leaderCommit, index of last new entry)
	if args.LeaderCommit > rf.commitIndex {
		if args.LeaderCommit < uint64(len(rf.log)-1) {
			rf.commitIndex = args.LeaderCommit
		} else {
			rf.commitIndex = uint64(len(rf.log) - 1)
		}
	}
}

// RequestVote RPC handler:
// Processes a RequestVote RPC received from a candidate.
// Replies with currentTerm and whether a vote is granted to the candidate.
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (3A, 3B).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// (1) Always reject request if args.term < currentTerm (5.1)
	if args.Term < rf.currentTerm {
		reply.VoteGranted = false
		reply.Term = rf.currentTerm
		return
	}

	// (2) If RPC request or response contains term T > currentTerm: set currentTerm = T,
	//     convert to follower (5.1)
	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.myRole = roleFollower
	}

	// Reply with my updated current term
	reply.Term = rf.currentTerm

	// (3) If votedFor is null or candidateId, and candidate's log is at least as up-to-date
	//     as receiver's log, grant vote (5.2 and 5.4)
	if rf.votedFor == -1 || rf.votedFor == args.CandidateId {
		// Up-to-date:
		// (a) If the logs have last entries with different terms, then
		//     the log with later term is more up-to-date
		// (b) If the logs end with the same term, then whichever log is
		//     longer is more up-to-date

		candidateLogTerm := args.LastLogTerm
		receiverLogTerm := rf.currentTerm

		if candidateLogTerm != receiverLogTerm {
			if candidateLogTerm < receiverLogTerm {
				// candidate is not up-to-date
				reply.VoteGranted = false
			} else {
				// candidate is more up-to-date
				reply.VoteGranted = true
			}
		} else {
			if args.LastLogIndex < uint64(len(rf.log)-1) {
				// candidate is not up-to-date
				reply.VoteGranted = false
			} else {
				// candidate is at least as up-to-date
				reply.VoteGranted = true
			}
		}
	} else {
		// Otherwise, I have voted for someone else for this term
		reply.VoteGranted = false
	}

	// (4) Reset election timer if vote is granted
	if reply.VoteGranted == true {
		rf.lastHeartbeat = time.Now()
	}
}

// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}

// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. even if the Raft instance has been killed,
// this function should return gracefully.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := true

	// Your code here (3B).

	return index, term, isLeader
}

// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids the
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

// Helper to generate randomized election timeout
func generateElectionTimeout() time.Duration {
	// 300-500 milliseconds
	return time.Duration(rand.Intn(200)+300) * time.Millisecond
}

// Helper to send heartbeat to a peer
func (rf *Raft) sendHeartbeatToPeer(peerId int, args *AppendEntriesArgs) {
	reply := &AppendEntriesReply{}
	ok := rf.sendAppendEntries(peerId, args, reply)
	if !ok {
		return
	}

	// If RPC request or response contains term T > currentTerm:
	// set currentTerm = T, convert to follower (Section 5.1);
	if reply.Term > rf.currentTerm {
		rf.mu.Lock()
		rf.currentTerm = reply.Term
		rf.myRole = roleFollower
		rf.votedFor = -1 // TODO: check
		rf.mu.Unlock()
		return
	}

}

// Upon election, the leader starts sending periodic heartbeats
func (rf *Raft) startHeartbeat() {
	for rf.killed() == false {
		rf.mu.Lock()

		if rf.myRole != roleLeader {
			rf.mu.Unlock()
			break
		}

		hbPeriod := time.Duration(100) * time.Millisecond
		lastHbSent := time.Now().Add(hbPeriod)

		if time.Since(lastHbSent) >= hbPeriod {
			// Send heartbeat to every server
			args := &AppendEntriesArgs{
				Term:         rf.currentTerm,
				LeaderId:     rf.me,
				PrevLogIndex: uint64(len(rf.log) - 1),
				PrevLogTerm:  rf.log[len(rf.log)-1].term,
				Entries:      make([]Log, 0),
				LeaderCommit: rf.commitIndex,
			}

			for peerId, _ := range rf.peers {
				if peerId != rf.me {
					go rf.sendHeartbeatToPeer(peerId, args)
				}
			}
		}

		rf.mu.Unlock()
	}
}

// Helper to send RequestVote RPC to a peer and collect vote
func (rf *Raft) requestVoteFromPeer(
	peerId int,
	args *RequestVoteArgs,
	votesGranted *uint64,
) {
	reply := &RequestVoteReply{}
	ok := rf.sendRequestVote(peerId, args, reply)
	if !ok {
		return
	}

	// If RPC request or response contains term T > currentTerm:
	// set currentTerm = T, convert to follower (Section 5.1);
	// terminate my election
	if reply.Term > rf.currentTerm {
		rf.mu.Lock()
		rf.currentTerm = reply.Term
		rf.myRole = roleFollower
		rf.votedFor = -1 // TODO: check
		rf.mu.Unlock()
		return
	}

	// Increment vote
	if reply.VoteGranted {
		atomic.AddUint64(votesGranted, 1)
	}

	rf.mu.Lock()
	defer rf.mu.Unlock()

	// Check if I have become leader
	if int(atomic.LoadUint64(votesGranted))*2 > len(rf.peers) {
		rf.myRole = roleLeader
		for i := 0; i < len(rf.peers); i++ {
			rf.nextIndex = append(rf.nextIndex, uint64(len(rf.log)+1)) // initialized to leader last log index + 1
			rf.matchIndex = append(rf.matchIndex, 0)                   // initialized to 0
		}

		// Initialize leader to start heartbeat
		go rf.startHeartbeat()
	}
}

// Helper to send RequestVote RPC to all peers, and determine vote result
// Note: caller should be holding lock
func (rf *Raft) startVoting() {
	// Atomic counter for total votes
	var votesGranted uint64 = 1 // there's always a vote from self

	args := &RequestVoteArgs{
		Term:         rf.currentTerm,
		CandidateId:  rf.me,
		LastLogIndex: uint64(len(rf.log) - 1),
		LastLogTerm:  rf.log[len(rf.log)-1].term,
	}

	// Get a vote from every peer
	for peerId, _ := range rf.peers {
		if peerId != rf.me {
			go rf.requestVoteFromPeer(peerId, args, &votesGranted)
		}
	}
}

// The ticker go routine starts a new election if this peer hasn't received
// heartsbeats recently.
// Create goroutine that will kickoff leader election periodically by
// sending out RequestVote RPCs when it hasn't heard from another peer
// for a while. This way a peer will learn who is the leader, if there
// is already a leader, or become the leader itself.
func (rf *Raft) ticker() {
	for rf.killed() == false {
		// Your code here to check if a leader election should
		// be started and to randomize sleeping time using
		// time.Sleep().

		rf.mu.Lock()

		if time.Since(rf.lastHeartbeat) >= rf.electionTimeout {
			// Only follower and candidates can start elections
			if rf.myRole != roleLeader {
				// (1) Convert to candidate
				rf.myRole = roleCandidate

				// (2) Increment currentTerm
				rf.currentTerm += 1

				// (3) Vote for self
				rf.votedFor = rf.me

				// (4) Reset election timer
				rf.lastHeartbeat = time.Now()
				// Each candidate restarts its randomized election timeout at the start of an election (5.2)
				rf.electionTimeout = generateElectionTimeout()

				// (5) Send RequestVote RPCs to all other servers
				rf.startVoting()

				// - If votes received from majority of servers: become leader
			}
		}

		// Sleep and wait for timeout
		remainingTime := rf.electionTimeout - time.Since(rf.lastHeartbeat)
		rf.mu.Unlock()
		time.Sleep(remainingTime)
	}
}

// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
func Make(
	peers []*labrpc.ClientEnd, // all Raft servers
	me int, // index of current server's port in peers
	persister *Persister,
	applyCh chan ApplyMsg,
) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// Your initialization code here (3A, 3B, 3C).
	rf.myRole = roleFollower // a server always starts as follower
	rf.electionTimeout = generateElectionTimeout()
	rf.lastHeartbeat = time.Now()

	rf.currentTerm = 0
	rf.votedFor = -1
	rf.log = make([]Log, 0)

	rf.commitIndex = 0
	rf.lastApplied = 0

	rf.nextIndex = make([]uint64, 0)
	rf.matchIndex = make([]uint64, 0)

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	// start ticker goroutine to start elections:
	go rf.ticker()

	return rf
}
