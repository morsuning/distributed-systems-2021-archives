6.824 - 2021年春季
6.824 实验3：容错键/值服务
A部分截止日期：4月9日周五 23:59
B部分截止日期：4月16日周五 23:59

简介
在这个实验中，你将使用实验2中的Raft库构建一个容错键/值存储服务。你的键/值服务将是一个复制状态机，由几个使用Raft进行复制的键/值服务器组成。只要大多数服务器存活并且可以通信，尽管有其他故障或网络分区，你的键/值服务应该继续处理客户端请求。实验3之后，你将实现Raft交互图中显示的所有部分（Clerk、Service和Raft）。

该服务支持三个操作：Put(key, value)、Append(key, arg)和Get(key)。它维护一个简单的键/值对数据库。键和值是字符串。Put()替换数据库中特定键的值，Append(key, arg)将arg追加到键的值，Get()获取键的当前值。对不存在键的Get应该返回空字符串。对不存在键的Append应该像Put一样。每个客户端通过带有Put/Append/Get方法的Clerk与服务通信。Clerk管理与服务器的RPC交互。

你的服务必须为对Clerk Get/Put/Append方法的应用程序调用提供强一致性。这是我们所说的强一致性。如果一次调用一个，Get/Put/Append方法应该表现得好像系统只有其状态的一个副本，每个调用应该观察由前面调用序列暗示的状态修改。对于并发调用，返回值和最终状态必须与操作按某种顺序一次执行一个相同。如果调用在时间上重叠，则调用是并发的，例如，如果客户端X调用Clerk.Put()，然后客户端Y调用Clerk.Append()，然后客户端X的调用返回。此外，调用必须观察在调用开始之前已完成的所有调用的效果（所以技术上我们要求线性化）。

强一致性对应用程序很方便，因为这意味着，非正式地说，所有客户端看到相同的状态，并且它们都看到最新状态。为单个服务器提供强一致性相对容易。如果服务是复制的，则更困难，因为所有服务器必须为并发请求选择相同的执行顺序，并且必须避免使用不是最新的状态回复客户端。

这个实验有两个部分。在A部分，你将实现服务，而不必担心Raft日志可以无限增长。在B部分，你将实现快照（论文第7节），这将允许Raft丢弃旧的日志条目。请在各自的截止日期前提交每个部分。

你应该重新阅读扩展Raft论文，特别是第7和第8节。为了获得更广泛的视角，可以看看Chubby、Paxos Made Live、Spanner、Zookeeper、Harp、Viewstamped Replication和Bolosky等人。

早开始。

开始准备
我们在src/kvraft中为你提供了骨架代码和测试。你需要修改kvraft/client.go、kvraft/server.go，也许还有kvraft/common.go。

要开始运行，执行以下命令。不要忘记git pull以获取最新软件。

```shell
$ cd ~/6.824
$ git pull
...
$ cd src/kvraft
$ go test -race
...
$
```

第A部分：不带快照的键/值服务（中等/困难）
你的每个键/值服务器（"kvservers"）将有一个关联的Raft对等节点。Clerk向其关联的Raft是leader的kvserver发送Put()、Append()和Get() RPC。kvserver代码将Put/Append/Get操作提交给Raft，以便Raft日志持有一系列Put/Append/Get操作。所有kvservers按顺序执行Raft日志中的操作，将操作应用到它们的键/值数据库；目的是让服务器维护键/值数据库的相同副本。

Clerk有时不知道哪个kvserver是Raft leader。如果Clerk向错误的kvserver发送RPC，或者如果它无法到达kvserver，Clerk应该通过向不同的kvserver发送重试。如果键/值服务将操作提交到其Raft日志（因此将操作应用到键/值状态机），leader通过响应其RPC向Clerk报告结果。如果操作未能提交（例如，如果leader被替换），服务器报告错误，Clerk用不同的服务器重试。

你的kvservers不应该直接通信；它们应该只通过Raft相互交互。

任务：你的第一个任务是实现在没有丢弃消息和没有失败服务器的情况下工作的解决方案。

你需要在client.go的Clerk Put/Append/Get方法中添加RPC发送代码，并在server.go中实现PutAppend()和Get() RPC处理程序。这些处理程序应该使用Start()在Raft日志中输入一个Op；你应该填写server.go中的Op结构定义，以便它描述Put/Append/Get操作。每个服务器应该执行Raft提交的Op命令，即当它们出现在applyCh上时。RPC处理程序应该注意到Raft提交了它的Op，然后回复RPC。

当你可靠地通过测试套件中的第一个测试时，你完成了这个任务："One client"。

调用Start()后，你的kvservers需要等待Raft完成一致性。已达成一致的命令到达applyCh。当PutAppend()和Get()处理程序使用Start()向Raft日志提交命令时，你的代码需要继续读取applyCh。注意kvserver与其Raft库之间的死锁。

你被允许向Raft ApplyMsg添加字段，以及向Raft RPC如AppendEntries添加字段，但是对于大多数实现来说，这不应该必要。

如果kvserver不是多数的一部分（以便它不提供陈旧数据），它不应该完成Get() RPC。一个简单的解决方案是在Raft日志中输入每个Get()（以及每个Put()和Append()）。你不必实现第8节中描述的只读操作优化。

最好从一开始就添加锁定，因为避免死锁的需要有时会影响整体代码设计。使用go test -race检查你的代码是否无竞态。

现在你应该修改你的解决方案以在网络和服务器故障面前继续运行。你将面临的一个问题是Clerk可能必须多次发送RPC，直到找到一个积极响应的kvserver。如果leader在将条目提交到Raft日志后立即失败，Clerk可能不会收到回复，因此可能将请求重新发送给另一个leader。每次调用Clerk.Put()或Clerk.Append()应该只导致一次执行，所以你必须确保重新发送不会导致服务器执行请求两次。

任务：添加代码来处理故障，并处理重复的Clerk请求，包括Clerk在一个任期中向kvserver leader发送请求，等待回复超时，然后在另一个任期中向新leader重新发送请求的情况。请求应该只执行一次。你的代码应该通过go test -run 3A -race测试。

你的解决方案需要处理已经为Clerk的RPC调用Start()但在请求提交到日志之前失去其领导权的leader。在这种情况下，你应该安排Clerk向其他服务器重新发送请求，直到找到新leader。一种方法是服务器通过注意到不同的请求出现在Start()返回的索引处，或者Raft的任期已经改变，来检测它失去了领导权。如果前leader被自己分区，它不会知道新leader；但同一分区中的任何客户端也无法与新leader通信，所以在这种情况下服务器和客户端无限期等待直到分区愈合是可以的。

你可能必须修改你的Clerk以记住哪个服务器是上次RPC的leader，并首先将下一个RPC发送到该服务器。这将避免每次RPC浪费时间搜索leader，这可能帮助你足够快地通过一些测试。

你需要唯一标识客户端操作，以确保键/值服务每个只执行一次。

你的重复检测方案应该快速释放服务器内存，例如，让每个RPC暗示客户端已经看到了其前一个RPC的回复。可以假设客户端一次只对Clerk进行一次调用。

你的代码现在应该通过实验3A测试，如下所示：

```shell
$ go test -run 3A -race
Test: one client (3A) ...
  ... Passed --  15.5  5  4576  903
Test: ops complete fast enough (3A) ...
  ... Passed --  15.7  3  3022    0
Test: many clients (3A) ...
  ... Passed --  15.9  5  5884 1160
Test: unreliable net, many clients (3A) ...
  ... Passed --  19.2  5  3083  441
Test: concurrent append to same key, unreliable (3A) ...
  ... Passed --   2.5  3   218   52
Test: progress in majority (3A) ...
  ... Passed --   1.7  5   103    2
Test: no progress in minority (3A) ...
  ... Passed --   1.0  5   102    3
Test: completion after heal (3A) ...
  ... Passed --   1.2  5    70    3
Test: partitions, one client (3A) ...
  ... Passed --  23.8  5  4501  765
Test: partitions, many clients (3A) ...
  ... Passed --  23.5  5  5692  974
Test: restarts, one client (3A) ...
  ... Passed --  22.2  5  4721  908
Test: restarts, many clients (3A) ...
  ... Passed --  22.5  5  5490 1033
Test: unreliable net, restarts, many clients (3A) ...
  ... Passed --  26.5  5  3532  474
Test: restarts, partitions, many clients (3A) ...
  ... Passed --  29.7  5  6122 1060
Test: unreliable net, restarts, partitions, many clients (3A) ...
  ... Passed --  32.9  5  2967  317
Test: unreliable net, restarts, partitions, random keys, many clients (3A) ...
  ... Passed --  35.0  7  8249  746
PASS
ok  	6.824/kvraft	290.184s
```

每条Passed之后的数字是实际时间（秒）、对等节点数量、发送的RPC数量（包括客户端RPC）和执行的键/值操作数量（Clerk Get/Put/Append调用）。

第B部分：带快照的键/值服务（困难）
按照你的代码现在的状态，重启的服务器重放完整的Raft日志以恢复其状态。然而，对于长期运行的服务器来说，永远记住完整的Raft日志是不现实的。相反，你将修改kvserver与Raft合作，使用实验2D的Raft的Snapshot()和CondInstallSnapshot来节省空间。

测试器向你的StartKVServer()传递maxraftstate。maxraftstate指示你的持久Raft状态的最大允许大小（以字节为单位）（包括日志，但不包括快照）。你应该将maxraftstate与persister.RaftStateSize()进行比较。每当你的键/值服务器检测到Raft状态大小接近此阈值时，它应该使用Snapshot保存快照，这又使用persister.SaveRaftState()。如果maxraftstate是-1，你不必快照。maxraftstate适用于你的Raft传递给persister.SaveRaftState()的GOB编码字节。

任务：修改你的kvserver，以便它检测持久化的Raft状态何时增长过大，然后将快照交给Raft。当kvserver服务器重启时，它应该从persister读取快照并从快照恢复其状态。

考虑kvserver应该何时对其状态进行快照以及快照中应该包含什么。Raft使用SaveStateAndSnapshot()将每个快照存储在persister对象中，连同相应的Raft状态。你可以使用ReadSnapshot()读取最新存储的快照。

你的kvserver必须能够检测日志中跨检查点的重复操作，所以你用于检测它们的任何状态都必须包含在快照中。

大写存储在快照中的结构的所有字段。

你的Raft库中可能有一些这个实验会暴露的错误。如果你对Raft实现进行更改，确保它继续通过所有实验2测试。

实验3测试的合理时间是400秒实际时间和700秒CPU时间。此外，go test -run TestSnapshotSize应该花费少于20秒实际时间。

你的代码应该通过3B测试（如此处的示例）以及3A测试（并且你的Raft必须继续通过实验2测试）。

```shell
$ go test -run 3B -race
Test: InstallSnapshot RPC (3B) ...
  ... Passed --   4.0  3   289   63
Test: snapshot size is reasonable (3B) ...
  ... Passed --   2.6  3  2418  800
Test: ops complete fast enough (3B) ...
  ... Passed --   3.2  3  3025    0
Test: restarts, snapshots, one client (3B) ...
  ... Passed --  21.9  5 29266 5820
Test: restarts, snapshots, many clients (3B) ...
  ... Passed --  21.5  5 33115 6420
Test: unreliable net, snapshots, many clients (3B) ...
  ... Passed --  17.4  5  3233  482
Test: unreliable net, restarts, snapshots, many clients (3B) ...
  ... Passed --  22.7  5  3337  471
Test: unreliable net, restarts, partitions, snapshots, many clients (3B) ...
  ... Passed --  30.4  5  2725  274
Test: unreliable net, restarts, partitions, snapshots, random keys, many clients (3B) ...
  ... Passed --  37.7  7  8378  681
PASS
ok  	6.824/kvraft	161.538s
```