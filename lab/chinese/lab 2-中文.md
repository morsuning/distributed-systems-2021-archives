6.824 - 2021年春季
6.824 实验2：Raft
第2A部分截止日期：3月5日周五 23:59
第2B部分截止日期：3月12日周五 23:59
第2C部分截止日期：3月19日周五 23:59
第2D部分截止日期：3月26日周五 23:59

简介
这是你将构建容错键/值存储系统的系列实验中的第一个。在这个实验中，你将实现Raft，一个复制状态机协议。在下一个实验中，你将在Raft之上构建一个键/值服务。然后你将把你的服务"分片"到多个复制状态机上以获得更高性能。

复制服务通过在多个副本服务器上存储其状态（即数据）的完整副本来实现容错。复制允许服务即使在其某些服务器遇到故障（崩溃或网络损坏或不稳定）时也能继续运行。挑战在于故障可能导致副本持有不同的数据副本。

Raft将客户端请求组织成一个序列，称为日志，并确保所有副本服务器看到相同的日志。每个副本按日志顺序执行客户端请求，将它们应用到服务状态的本地副本上。由于所有活跃的副本看到相同的日志内容，它们都以相同的顺序执行相同的请求，从而继续具有相同的服务状态。如果服务器失败但后来恢复，Raft负责将其日志更新到最新状态。只要至少大多数服务器存活并且可以相互通信，Raft将继续运行。如果没有这样的多数，Raft将无法取得进展，但一旦多数可以再次通信，它将从上次停止的地方继续。

在这个实验中，你将把Raft实现为一个带有相关方法的Go对象类型，意在用作更大服务中的模块。一组Raft实例通过RPC相互通信以维护复制的日志。你的Raft接口应该支持无限序列的编号命令，也称为日志条目。条目用索引编号。给定索引的日志条目最终将被提交。那时，你的Raft应该将日志条目发送给更大的服务以执行。

你应该遵循扩展Raft论文中的设计，特别关注图2。你将实现论文中的大部分内容，包括保存持久状态和节点失败重启后读取它。你不会实现集群成员更改（第6节）。

你可能会发现这个指南有用，以及这个关于并发锁定和结构的建议。为了获得更广泛的视角，可以看看Paxos、Chubby、Paxos Made Live、Spanner、Zookeeper、Harp、Viewstamped Replication和Bolosky等人。（注意：学生指南是几年前写的，特别是第2D部分已经发生了变化。在盲目遵循之前，确保你理解为什么特定的实现策略是有意义的！）

我们还提供了一个Raft交互图，可以帮助阐明你的Raft代码如何与其上层交互。

这个实验分四部分提交。你必须在相应的截止日期前提交每个部分。

开始准备
如果你已经完成了实验1，你已经有了实验源代码的副本。如果没有，你可以在实验1说明中找到通过git获取源代码的说明。

我们在src/raft/raft.go中提供了骨架代码。我们还提供了一组测试，你应该用它们来驱动你的实现工作，我们将用它们来评分你提交的实验。测试在src/raft/test_test.go中。

要开始运行，执行以下命令。不要忘记git pull以获取最新软件。

```shell
$ cd ~/6.824
$ git pull
...
$ cd src/raft
$ go test -race
Test (2A): initial election ...
--- FAIL: TestInitialElection2A (5.04s)
        config.go:326: expected one leader, got none
Test (2A): election after network failure ...
--- FAIL: TestReElection2A (5.03s)
        config.go:326: expected one leader, got none
...
$
```

代码
通过向raft/raft.go添加代码来实现Raft。在该文件中，你会发现骨架代码，以及发送和接收RPC的示例。

你的实现必须支持以下接口，测试器和（最终）你的键/值服务器将使用这个接口。你可以在raft.go的注释中找到更多细节。

```go
// 创建一个新的Raft服务器实例：
rf := Make(peers, me, persister, applyCh)

// 开始就新日志条目达成一致：
rf.Start(command interface{}) (index, term, isleader)

// 询问Raft其当前任期，以及它是否认为自己是leader
rf.GetState() (term, isLeader)

// 每次新条目提交到日志时，每个Raft对等节点
// 应该向服务（或测试器）发送一个ApplyMsg。
type ApplyMsg
```

服务调用Make(peers,me,…)来创建Raft对等节点。peers参数是Raft对等节点（包括这个）的网络标识符数组，用于RPC。me参数是这个对等节点在peers数组中的索引。Start(command)要求Raft开始处理以将命令追加到复制的日志中。Start()应该立即返回，不等待日志追加完成。服务期望你的实现为每个新提交的日志条目向Make()的applyCh通道参数发送一个ApplyMsg。

raft.go包含发送RPC（sendRequestVote()）和处理传入RPC（RequestVote()）的示例代码。你的Raft对等节点应该使用labrpc Go包（源代码在src/labrpc中）交换RPC。测试器可以告诉labrpc延迟RPC、重新排序它们并丢弃它们以模拟各种网络故障。虽然你可以临时修改labrpc，但确保你的Raft与原始labrpc一起工作，因为那是我们将用来测试和评分你的实验的内容。你的Raft实例必须只与RPC交互；例如，它们不允许使用共享Go变量或文件进行通信。

后续实验建立在这个实验的基础上，所以给自己足够的时间编写可靠的代码很重要。

第2A部分：leader选举（中等）
任务：实现Raft leader选举和心跳（没有日志条目的AppendEntries RPC）。第2A部分的目标是选举出一个单一的leader，如果没有故障，leader保持为leader，如果旧leader失败或到/来自旧leader的数据包丢失，新的leader接管。运行go test -run 2A -race来测试你的2A代码。

你不能直接运行你的Raft实现；相反，你应该通过测试器运行它，即go test -run 2A -race。

遵循论文的图2。此时你关心发送和接收RequestVote RPC、与选举相关的服务器规则，以及与leader选举相关的状态。

将图2中用于leader选举的状态添加到raft.go的Raft结构中。你还需要定义一个结构来保存每个日志条目的信息。

填充RequestVoteArgs和RequestVoteReply结构。修改Make()创建一个后台goroutine，当一段时间没有听到来自其他对等节点的消息时，通过发送RequestVote RPC定期启动leader选举。这样，一个对等节点将了解谁是leader，如果已经有leader的话，或者自己成为leader。实现RequestVote() RPC处理程序，以便服务器相互投票。

要实现心跳，定义一个AppendEntries RPC结构（尽管你可能还不需要所有参数），并让leader定期发送它们。编写一个AppendEntries RPC处理方法，重置选举超时，以便在其他服务器已经选举出leader时不会前进成为leader。

确保不同对等节点中的选举超时不会总是在同一时间触发，否则所有对等节点只会投票给自己，没有人会成为leader。

测试器要求leader每秒发送心跳RPC不超过十次。

测试器要求你的Raft在旧leader失败后的五秒内选举出新leader（如果大多数对等节点仍然可以通信）。但是，请记住，在分裂投票的情况下（如果数据包丢失或候选人不幸运地选择相同的随机退避时间），leader选举可能需要多轮。你必须选择足够短的选举超时（以及心跳间隔），以便即使需要多轮，选举也很可能在五秒内完成。

论文的第5.2节提到了150到300毫秒范围内的选举超时。这样的范围只有在leader发送心跳的频率显著高于每150毫秒一次时才有意义。因为测试器限制你每秒10次心跳，你必须使用比论文的150到300毫秒更大的选举超时，但不能太大，否则你可能无法在五秒内选举出leader。

你可能会发现Go的rand有用。

你需要编写定期或延迟后采取行动的代码。最简单的方法是创建一个调用time.Sleep()的循环的goroutine；（参见Make()为此目的创建的ticker() goroutine）。不要使用Go的time.Timer或time.Ticker，它们很难正确使用。

指导页面有一些关于如何开发和调试代码的提示。

如果你的代码难以通过测试，再次阅读论文的图2；leader选举的完整逻辑分布在图的多个部分。

不要忘记实现GetState()。

当测试器永久关闭实例时，它会调用你的Raft的rf.Kill()。你可以使用rf.killed()检查Kill()是否已被调用。你可能想在所有循环中这样做，以避免死Raft实例打印令人困惑的消息。

Go RPC只发送名称以大写字母开头的结构字段。子结构也必须有大写的字段名称（例如数组中日志记录的字段）。labgob包会警告你这一点；不要忽略警告。

确保在提交第2A部分之前通过了2A测试，这样你会看到类似这样的内容：

```shell
$ go test -run 2A -race
Test (2A): initial election ...
  ... Passed --   4.0  3   32    9170    0
Test (2A): election after network failure ...
  ... Passed --   6.1  3   70   13895    0
PASS
ok      raft    10.187s
$
```

每条"Passed"行包含五个数字；这些是测试花费的时间（秒）、Raft对等节点数量（通常是3或5）、测试期间发送的RPC数量、RPC消息中的总字节数，以及Raft报告已提交的日志条目数量。你的数字可能与这里显示的不同。如果你愿意，可以忽略这些数字，但它们可能帮助你合理性检查你的实现发送的RPC数量。对于所有实验2、3和4，如果所有测试（go test）花费超过600秒，或任何单个测试花费超过120秒，评分脚本将让你的解决方案失败。

第2B部分：日志（困难）
任务：实现leader和follower代码来追加新的日志条目，使go test -run 2B -race测试通过。

运行git pull获取最新的实验软件。

你的第一个目标应该是通过TestBasicAgree2B()。首先实现Start()，然后编写通过AppendEntries RPC发送和接收新日志条目的代码，遵循图2。

你需要实现选举限制（论文第5.4.1节）。

在早期第2B测试中未能达成一致的一个原因，即使leader活着也举行重复选举。寻找选举计时器管理中的错误，或者赢得选举后没有立即发送心跳。

你的代码可能有重复检查某些事件的循环。不要让这些循环连续执行而不暂停，因为那会减慢你的实现速度以至于测试失败。使用Go的条件变量，或在每个循环迭代中插入time.Sleep(10 * time.Millisecond)。

为未来的实验帮自己一个忙，编写（或重写）干净清晰的代码。对于想法，重新访问我们的指导页面，其中有关于如何开发和调试代码的提示。

如果你测试失败，查看config.go和test_test.go中的测试代码，以更好地理解测试在测试什么。config.go还说明了测试器如何使用Raft API。

即将到来的实验的测试如果你的代码运行太慢可能会失败。你可以用time命令检查你的解决方案使用了多少实际时间和CPU时间。这是典型的输出：

```shell
$ time go test -run 2B
Test (2B): basic agreement ...
  ... Passed --   1.6  3   18    5158    3
Test (2B): RPC byte count ...
  ... Passed --   3.3  3   50  115122   11
Test (2B): agreement despite follower disconnection ...
  ... Passed --   6.3  3   64   17489    7
Test (2B): no agreement if too many followers disconnect ...
  ... Passed --   4.9  5  116   27838    3
Test (2B): concurrent Start()s ...
  ... Passed --   2.1  3   16    4648    6
Test (2B): rejoin of partitioned leader ...
  ... Passed --   8.1  3  111   26996    4
Test (2B): leader backs up quickly over incorrect follower logs ...
  ... Passed --  28.6  5 1342  953354  102
Test (2B): RPC counts aren't too high ...
  ... Passed --   3.4  3   30    9050   12
PASS
ok      raft    58.142s

real    0m58.475s
user    0m2.477s
sys     0m1.406s
$
```

"ok raft 58.142s"意味着Go测量2B测试花费的时间是58.142秒实际（墙上时钟）时间。"user 0m2.477s"意味着代码消耗了2.477秒CPU时间，或花费在实际执行指令上的时间（而不是等待或睡眠）。如果你的解决方案对于2B测试使用超过一分钟的实际时间，或超过5秒的CPU时间，你可能会在以后遇到麻烦。寻找花费在睡眠或等待RPC超时的时间，运行而不睡眠或等待条件或通道消息的循环，或发送的大量RPC。

第2C部分：持久化（困难）
如果基于Raft的服务器重启，它应该从上次停止的地方恢复服务。这要求Raft保持持久状态，在重启后仍然存在。论文的图2提到了哪些状态应该是持久的。

一个真实的实现会在每次更改时将Raft的持久状态写入磁盘，并在重启后从磁盘读取状态。你的实现不会使用磁盘；相反，它将从Persister对象（参见persister.go）保存和恢复持久状态。调用Raft.Make()的人提供一个最初保存Raft最近持久状态（如果有）的Persister。Raft应该从该Persister初始化其状态，并应该使用它在每次状态更改时保存其持久状态。使用Persister的ReadRaftState()和SaveRaftState()方法。

任务：通过添加保存和恢复持久状态的代码来完成raft.go中的persist()和readPersist()函数。你需要将状态编码（或"序列化"）为字节数组以传递给Persister。使用labgob编码器；参见persist()和readPersist()中的注释。labgob类似于Go的gob编码器，但如果你尝试编码有小写字段名的结构，它会打印错误消息。

任务：在实现更改持久状态的点插入persist()调用。完成此操作后，你应该通过剩余的测试。

为了避免内存耗尽，Raft必须定期丢弃旧的日志条目，但在下一个实验之前你不必担心这一点。

运行git pull获取最新的实验软件。

许多2C测试涉及服务器失败和网络丢失RPC请求或回复。这些事件是非确定性的，即使你的代码有错误，你也可能幸运地通过测试。通常运行测试几次会暴露这些错误。

你可能需要一次备份多个nextIndex条目的优化。查看扩展Raft论文的第7页底部和第8页顶部（用灰线标记）。论文对细节含糊不清；你需要填补空白，也许借助6.824 Raft讲座的帮助。

虽然2C只要求你实现持久化和快速日志回溯，但2C测试失败可能与你的实现的先前部分有关。即使你一直通过2A和2B测试，你仍然可能有在2C测试中暴露的选举或日志错误。

你的代码应该通过所有2C测试（如下所示），以及2A和2B测试。

```shell
$ go test -run 2C -race
Test (2C): basic persistence ...
  ... Passed --   7.2  3  206   42208    6
Test (2C): more persistence ...
  ... Passed --  23.2  5 1194  198270   16
Test (2C): partitioned leader and one follower crash, leader restarts ...
  ... Passed --   3.2  3   46   10638    4
Test (2C): Figure 8 ...
  ... Passed --  35.1  5 9395 1939183   25
Test (2C): unreliable agreement ...
  ... Passed --   4.2  5  244   85259  246
Test (2C): Figure 8 (unreliable) ...
  ... Passed --  36.3  5 1948 4175577  216
Test (2C): churn ...
  ... Passed --  16.6  5 4402 2220926 1766
Test (2C): unreliable churn ...
  ... Passed --  16.5  5  781  539084  221
PASS
ok      raft    142.357s
$
```

在提交之前多次运行测试并检查每次运行都打印PASS是个好主意。

```shell
$ for i in {0..10}; do go test; done
```

第2D部分：日志压缩（困难）
按照你的代码现在的状态，重启的服务器重放完整的Raft日志以恢复其状态。然而，对于长期运行的服务来说，永远记住完整的Raft日志是不现实的。相反，你将修改Raft以合作节省空间：服务将定期持久存储其当前状态的"快照"，Raft将丢弃快照之前的日志条目。当服务远远落后于leader并且必须追赶时，服务首先安装快照，然后重放快照创建点之后的日志条目。扩展Raft论文的第7节概述了方案；你必须设计细节。

你可能会发现参考Raft交互图有助于理解复制服务和Raft如何通信。

为了支持快照，我们需要服务和Raft库之间的接口。Raft论文没有指定这个接口，几种设计是可能的。为了允许简单的实现，我们决定在服务和Raft之间使用以下接口：

Snapshot(index int, snapshot []byte)
CondInstallSnapshot(lastIncludedTerm int, lastIncludedIndex int, snapshot []byte) bool

服务调用Snapshot()将其状态的快照传达给Raft。快照包括直到并包括index的所有信息。这意味着相应的Raft对等节点不再需要通过（并包括）index的日志。你的Raft实现应该尽可能修剪其日志。你必须修改你的Raft代码在只存储日志尾部的情况下运行。

如扩展Raft论文中所讨论的，Raft leader有时必须告诉落后的Raft对等节点通过安装快照来更新其状态。你需要实现InstallSnapshot RPC发送器和处理程序，在这种情况下安装快照。这与AppendEntries形成对比，AppendEntries发送日志条目，然后由服务逐个应用。

注意InstallSnapshot RPC在Raft对等节点之间发送，而提供的骨架函数Snapshot/CondInstallSnapshot是服务用来与Raft通信的。

当follower接收并处理InstallSnapshot RPC时，它必须使用Raft将包含的快照交给服务。InstallSnapshot处理程序可以使用applyCh将快照发送给服务，方法是将快照放在ApplyMsg中。服务从applyCh读取，并调用CondInstallSnapshot与快照，告诉Raft服务正在切换到传递的快照状态，Raft应该同时更新其日志。（参见config.go中的applierSnap()，看看测试器服务如何做到这一点）

如果快照是旧的快照（即，如果Raft在快照的lastIncludedTerm/lastIncludedIndex之后处理了条目），CondInstallSnapshot应该拒绝安装快照。这是因为Raft在处理InstallSnapshot RPC之后，在服务调用CondInstallSnapshot之前可能处理其他RPC并在applyCh上发送消息。Raft回到旧快照是不可以的，所以必须拒绝旧快照。当你的实现拒绝快照时，CondInstallSnapshot应该只返回false，以便服务知道它不应该切换到快照。

如果快照是最近的，那么Raft应该修剪其日志，持久化新状态，返回true，服务应该在处理applyCh上的下一条消息之前切换到快照。

CondInstallSnapshot是更新Raft和服务状态的一种方法；服务和raft之间的其他接口也是可能的。这种特殊设计允许你的实现在一个地方进行检查是否必须安装快照，并将服务和Raft原子地切换到快照。你可以自由地实现Raft，使CondInstallSnapShot总是返回true；如果你的实现通过测试，你获得满分。

任务：修改你的Raft代码以支持快照：实现Snapshot、CondInstallSnapshot和InstallSnapshot RPC，以及对Raft的更改以支持这些（例如，继续使用修剪的日志运行）。当它通过2D测试和所有实验2测试时，你的解决方案就完成了。（注意实验3将比实验2更彻底地测试快照，因为实验3有一个真实服务来压力测试Raft的快照。）

在单个InstallSnapshot RPC中发送整个快照。不要实现图13的用于分割快照的偏移机制。

Raft必须以允许Go垃圾收集器释放和重用内存的方式丢弃旧的日志条目；这要求没有对丢弃的日志条目的可达引用（指针）。

Raft日志不能再使用日志条目的位置或日志的长度来确定日志条目索引；你需要使用独立于日志位置的索引方案。

即使日志被修剪，你的实现仍然需要正确发送AppendEntries RPC中新条目之前条目的任期和索引；这可能需要保存和引用最新快照的lastIncludedTerm/lastIncludedIndex（考虑这是否应该被持久化）。

Raft必须使用SaveStateAndSnapshot()在persister对象中存储每个快照。

实验2测试集（2A+2B+2C+2D）的合理时间是8分钟实际时间和一分钟半CPU时间。
