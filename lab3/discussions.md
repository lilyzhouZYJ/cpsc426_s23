# 3A-2

**(1) Construct and describe a scenario with a Raft cluster of 3 or 5 nodes where the leader election protocol fails to elect a leader. Hint: in your description, you may decide when timers time out or not time out, or arbitrate when RPCs get sent or processed.**

Suppose we have 3 nodes: N1, N2, and N3. Suppose our omniscient adversary who controls the `rand` package has made sure that N2 and N3 will always get the exact same election timeout.

- Suppose we start in Term 1 with N1 as the leader, but N1 fails.
- N2 and N3 time out simultaneously, convert to candidate, increment their terms to 2, and vote for themselves.
- N2 sends a `RequestVote` to N3, and N3 sends a `RequestVote` to N2. But because both nodes have already voted for themselves for Term 2, neither will be able to receive a vote from the other node.
- Both N2 and N3 will hit election timeout at the same time, and both will start a new election. But it will end with the exact same result, with neither node being able to obtain a vote from the other.
- Hence, this repeated election process would go on forever, with no leader elected.

**(2) In practice, why is this not a major concern? i.e., how does Raft get around this theoretical possibility?**

Raft gets around this theoretical possibility by using randomized election timeout. Election timeouts are chosen randomly from a fixed interval, which makes it less likely that multiple nodes will time out simultaneously, hence reducing the likelihood of a split vote. Moreover, at the start of each election, the candidate will restart its randomized election timeout, and it will only start a new election after that timeout has elapsed. This reduces the likelihood that the new election would result in another split vote. With these measures, it is highly unlikely that Raft would hit a scenario where the leader election fails to elect a leader.

# ExtraCredit1

**Another issue that affects Raft liveness in the real world (e.g., this Cloudfare outage---though this is not a Byzantine failure.) is related to "term-inflation". Dr. Diego Ongaro described the problem and his idea of addressing this in Section 9.6 of his thesis; here is MongoDB's detailed account of the "Pre-Vote" modification they implemented; this blog post further describe the ramification and limitations of Pre-Vote and CheckQuorum. Does the scenario you constructed above resolve if the Raft instances implement Pre-Vote and CheckQuorum? If so, could you construct a scenario where Raft leader election can be theoretically stuck even with Pre-Vote and CheckQuorum? If not, explain why not. Include your response under the heading ExtraCredit1 in discussions.md.**

Implementing Pre-Vote and CheckQuorum would not be able to resolve the situation above. Such measures could not resolve the liveness issue caused by servers repeatedly timing out at the same time. However, this scenario only occurs when the timeout always happens at the same time, which is basically impossible because of the randomized election timeout duration.