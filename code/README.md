# MIT 6.824 实验

## 实验内容

### Lab 1

https://pdos.csail.mit.edu/6.824/labs/lab-mr.html

Map Reduce 系统

修改文件
/mr/master.go
/mr/rpc.go
/mr/worker.go

### Lab 2
https://pdos.csail.mit.edu/6.824/labs/lab-raft.html

#### 实验分析
Raft 将客户端请求组织成一个序列，称为日志，并确保所有副本服务器看到相同的日志。每个副本按日志顺序执行客户端请求，将它们应用到服务状态的本地副本。由于所有活动副本看到相同的日志内容，它们都以相同的顺序执行相同的请求，从而继续具有相同的服务状态。如果服务器出现故障但稍后恢复，Raft 会负责更新其日志。只要至少大多数服务器还活着并且可以相互通信，Raft 就会继续运行。如果没有这样的多数，Raft 将不会有任何进展，但会在多数可以再次通信时从停止的地方开始。

#### Lab 2A
实现 Raft Leader 选举和心跳

完善requestVoteArgs和RequestVoteReply两个数据结构。
完成Make()创建raft实例，和RequestVote请求投票两个方法。
定义一个AppendEntries rpc struct，完成AppendEntries 的 Rpc调用。
为entries定义一个struct

修改文件
src/raft/raft.go

提示
你不能轻易地直接运行你的 Raft 实现；相反，您应该通过测试仪运行它，即 go test -run 2A -race。
按照论文的图 2。此时您关心发送和接收 RequestVote RPC，与选举相关的服务器规则，以及与领导选举相关的状态，
在 raft.go 的 Raft 结构体中添加图 2 中的领导人选举状态。您还需要定义一个结构来保存有关每个日志条目的信息。
填写 RequestVoteArgs 和 RequestVoteReply 结构。修改 Make() 以创建一个后台 goroutine，当它有一段时间没有收到其他对等方的消息时，它将通过发送 RequestVote RPC 来定期启动领导者选举。通过这种方式，peer 将了解谁是领导者，如果已经有领导者，或者自己成为领导者。实现 RequestVote() RPC 处理程序，以便服务器相互投票。
要实现心跳，请定义一个 AppendEntries RPC 结构（尽管您可能还不需要所有参数），并让领导者定期发送它们。 Write an AppendEntries RPC handler method that resets the election timeout so that other servers don't step forward as leaders when one has already been elected.
确保不同 peer 的选举超时不会总是同时触发，否则所有 peer 只会为自己投票，没有人会成为领导者。
测试者要求领导者每秒发送心跳 RPC 不超过十次。
测试者要求你的 Raft 在旧领导者失败后的 5 秒内选举新领导者（如果大多数对等点仍然可以通信）。但是请记住，如果发生分裂投票（如果数据包丢失或候选人不幸选择相同的随机退避时间，可能会发生这种情况），领导者选举可能需要多轮投票。您必须选择足够短的选举超时（以及心跳间隔），即使选举需要多轮，也很可能在不到五秒的时间内完成。
该论文的第 5.2 节提到了 150 到 300 毫秒范围内的选举超时。只有当领导者发送心跳的频率远高于每 150 毫秒一次时，这样的范围才有意义。因为测试器将您限制为每秒 10 次心跳，您将不得不使用比论文中的 150 到 300 毫秒大的选举超时时间，但不要太大，因为那样您可能无法在 5 秒内选举出领导者。
您可能会发现 Go 的 rand 很有用。
您需要编写定期或延迟后采取行动的代码。最简单的方法是创建一个带有循环调用 time.Sleep(); 的 goroutine。 （请参阅 Make() 为此目的创建的 ticker() 协程）。不要使用 Go 的 time.Timer 或 time.Ticker，它们很难正确使用。
Guidance 页面提供了一些关于如何开发和调试代码的提示。
如果您的代码无法通过测试，请再次阅读论文的图 2；领导选举的完整逻辑分布在图中的多个部分。
不要忘记实现 GetState()。
测试人员在永久关闭实例时调用您的 Raft 的 rf.Kill()。您可以使用 rf.killed() 检查是否已调用 Kill()。您可能希望在所有循环中都这样做，以避免死 Raft 实例打印混乱的消息。
Go RPC 只发送名称以大写字母开头的结构体字段。子结构还必须具有大写的字段名称（例如数组中的日志记录字段）。 labgob 包会警告你这一点；不要忽略警告。

分析


实现