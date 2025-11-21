6.824 - 2021年春季
6.824 实验4：分片键/值服务
A部分截止日期：4月23日 23:59
B部分截止日期：5月14日 23:59

简介
你可以基于自己的想法做最终项目，或者这个实验。

在这个实验中，你将构建一个键/值存储系统，该系统"分片"或分区键到一组副本组上。分片是键/值对的子集；例如，所有以"a"开头的键可能是一个分片，所有以"b"开头的键是另一个，等等。分片的原因是性能。每个副本组只处理几个分片的put和get，组并行操作；因此系统总吞吐量（每单位时间的put和get）与组数量成正比增加。

你的分片键/值存储将有两个主要组件。首先，一组副本组。每个副本组负责一部分分片。副本由少数使用Raft复制组分片的服务器组成。第二个组件是"分片控制器"。分片控制器决定哪个副本组应该服务每个分片；这个信息称为配置。配置随时间变化。客户端咨询分片控制器以找到键的副本组，副本组咨询控制器以了解要服务哪些分片。整个系统有一个分片控制器，使用Raft实现为容错服务。

分片存储系统必须能够在副本组之间移动分片。一个原因是一些组可能变得比其他组更负载，所以需要移动分片以平衡负载。另一个原因是副本组可能加入和离开系统：可能添加新的副本组以增加容量，或者现有的副本组可能下线进行维修或退役。

这个实验的主要挑战将是处理重新配置——分片到组分配的变化。在单个副本组内，所有组成员必须就重新配置相对于客户端Put/Append/Get请求发生的时间达成一致。例如，Put可能在重新配置大约同时到达，重新配置导致副本组停止负责保存Put键的分片。组中的所有副本必须就Put是在重新配置之前还是之后发生达成一致。如果在之前，Put应该生效，分片的新所有者将看到其效果；如果在之后，Put不会生效，客户端必须在新所有者处重试。推荐的方法是让每个副本组使用Raft不仅记录Put、Append和Get的序列，还记录重新配置的序列。你需要确保在任何时候最多只有一个副本组为每个分片服务请求。

重新配置还需要副本组之间的交互。例如，在配置10中，组G1可能负责分片S1。在配置11中，组G2可能负责分片S1。在从10到11的重新配置期间，G1和G2必须使用RPC将分片S1的内容（键/值对）从G1移动到G2。

注意：只能使用RPC进行客户端和服务器之间的交互。例如，不允许你的服务器的不同实例共享Go变量或文件。

注意：这个实验使用"配置"来指代分片到副本组的分配。这与Raft集群成员更改不同。你不必实现Raft集群成员更改。

这个实验的总体架构（配置服务和一组副本组）遵循与Flat Datacenter Storage、BigTable、Spanner、FAWN、Apache HBase、Rosebud、Spinnaker和许多其他系统相同的通用模式。不过，这些系统在许多细节上与这个实验不同，并且通常也更复杂和功能更强大。例如，实验不会演变每个Raft组中的对等节点集合；其数据和查询模型非常简单；分片交接很慢，不允许并发客户端访问。

注意：你的实验4分片服务器、实验4分片控制器和实验3 kvraft都必须使用相同的Raft实现。作为评分实验4的一部分，我们将重新运行实验2和实验3测试，你在旧测试上的分数将计入你的总实验4成绩。这些测试占你总实验4成绩的10分。

开始准备
重要：执行git pull获取最新的实验软件。

我们在src/shardctrler和src/shardkv中为你提供了骨架代码和测试。

要开始运行，执行以下命令：

```shell
$ cd ~/6.824
$ git pull
...
$ cd src/shardctrler
$ go test
--- FAIL: TestBasic (0.00s)
        test_test.go:11: wanted 1 groups, got 0
FAIL
exit status 1
FAIL    shardctrler     0.008s
$
```

当你完成后，你的实现应该通过src/shardctrler目录中的所有测试，以及src/shardkv中的所有测试。

第A部分：分片控制器（30分）（简单）
首先你将在shardctrler/server.go和client.go中实现分片控制器。完成后，你应该通过shardctrler目录中的所有测试：

```shell
$ cd ~/6.824/src/shardctrler
$ go test -race
Test: Basic leave/join ...
  ... Passed
Test: Historical queries ...
  ... Passed
Test: Move ...
  ... Passed
Test: Concurrent leave/join ...
  ... Passed
Test: Minimal transfers after joins ...
  ... Passed
Test: Minimal transfers after leaves ...
  ... Passed
Test: Multi-group join/leave ...
  ... Passed
Test: Concurrent multi leave/join ...
  ... Passed
Test: Minimal transfers after multijoins ...
  ... Passed
Test: Minimal transfers after multileaves ...
  ... Passed
Test: Check Same config on servers ...
  ... Passed
PASS
ok  	6.824/shardctrler	5.863s
$
```

shardctrler管理一系列编号的配置。每个配置描述一组副本组和分片到副本组的分配。每当这个分配需要改变时，分片控制器创建一个具有新分配的新配置。键/值客户端和服务器在想知道当前（或过去）配置时联系shardctrler。

你的实现必须支持shardctrler/common.go中描述的RPC接口，包括Join、Leave、Move和Query RPC。这些RPC旨在允许管理员（和测试）控制shardctrler：添加新的副本组，消除副本组，以及在副本组之间移动分片。

Join RPC由管理员用来添加新的副本组。它的参数是从唯一的、非零副本组标识符（GID）到服务器名称列表的一组映射。shardctrler应该通过创建一个包含新副本组的新配置来反应。新配置应该尽可能在完整组集之间平均分配分片，并且应该移动尽可能少的分片来实现这个目标。shardctrler应该允许重用GID，如果它不是当前配置的一部分（即GID应该被允许Join，然后Leave，然后再Join）。

Leave RPC的参数是先前加入的组的GID列表。shardctrler应该创建一个不包含那些组的新配置，并将那些组的分片分配给剩余的组。新配置应该尽可能在组之间平均分配分片，并且应该移动尽可能少的分片来实现这个目标。

Move RPC的参数是分片号和GID。shardctrler应该创建一个分片分配给该组的新配置。Move的目的是允许我们测试你的软件。Move之后的Join或Leave可能会撤销Move，因为Join和Leave会重新平衡。

Query RPC的参数是配置号。shardctrler回复具有该号的配置。如果号是-1或大于已知最大配置号，shardctrler应该回复最新配置。Query(-1)的结果应该反映shardctrler在收到Query(-1) RPC之前完成处理的每个Join、Leave或Move RPC。

第一个配置应该编号为0。它应该不包含组，所有分片都应该分配给GID 0（无效GID）。下一个配置（响应Join RPC创建）应该编号为1，等等。通常分片数量显著多于组数量（即每个组将服务多个分片），以便可以以相当细的粒度转移负载。

任务：你的任务是在shardctrler/目录的client.go和server.go中实现上述指定的接口。你的shardctrler必须是容错的，使用你实验2/3中的Raft库。注意我们在评分实验4时会重新运行实验2和3的测试，所以确保你没有在Raft实现中引入错误。当你通过shardctrler/中的所有测试时，你完成了这个任务。

从你的kvraft服务器的精简副本开始。

你应该为分片控制器的RPC实现重复的客户端请求检测。shardctrler测试不测试这个，但shardkv测试稍后将在不可靠网络上使用你的shardctrler；如果你的shardctrler不过滤重复的RPC，你可能在通过shardkv测试时遇到麻烦。

执行分片重新平衡的状态机代码需要是确定性的。在Go中，map迭代顺序不是确定性的。

Go map是引用。如果你将一个map类型的变量分配给另一个，两个变量引用同一个map。因此，如果你想基于前一个创建新的Config，你需要创建新的map对象（使用make()）并单独复制键和值。

Go竞态检测器（go test -race）可能帮助你找到错误。

第B部分：分片键/值服务器（60分）（困难）
重要：执行git pull获取最新的实验软件。

现在你将构建shardkv，一个分片容错键/值存储系统。你将修改shardkv/client.go、shardkv/common.go和shardkv/server.go。

每个shardkv服务器作为副本组的一部分运行。每个副本组为一些键空间分片服务Get、Put和Append操作。使用client.go中的key2shard()查找键属于哪个分片。多个副本组合作服务完整的分片集。shardctrler服务的单个实例将分片分配给副本组；当这个分配改变时，副本组必须相互交接分片，同时确保客户端不会看到不一致的响应。

你的存储系统必须为使用其客户端接口的应用程序提供线性化接口。也就是说，在shardkv/client.go中对Clerk.Get()、Clerk.Put()和Clerk.Append()方法已完成的应用程序调用必须看起来以相同顺序影响了所有副本。Clerk.Get()应该看到最近对相同键的Put/Append写入的值。即使在Gets和Puts大约在配置更改同时到达时，这也必须成立。

你的每个分片只需要在分片的Raft副本组中的大多数服务器存活并且可以相互通信，并且可以与大多数shardctrler服务器通信时才能取得进展。你的实现必须运行（服务请求并能够根据需要重新配置），即使某些副本组中的少数服务器死机、暂时不可用或缓慢。

shardkv服务器只是单个副本组的成员。给定副本组中的服务器集永远不会改变。

我们向你提供client.go代码，它将每个RPC发送给负责RPC键的副本组。如果副本组说它不负责该键，它会重试；在这种情况下，客户端代码向分片控制器请求最新配置并再次尝试。你必须修改client.go作为处理重复客户端RPC支持的一部分，很像kvraft实验。

当你完成后，你的代码应该通过除挑战测试之外的所有shardkv测试：

```shell
$ cd ~/6.824/src/shardkv
$ go test -race
Test: static shards ...
  ... Passed
Test: join then leave ...
  ... Passed
Test: snapshots, join, and leave ...
  ... Passed
Test: servers miss configuration changes...
  ... Passed
Test: concurrent puts and configuration changes...
  ... Passed
Test: more concurrent puts and configuration changes...
  ... Passed
Test: concurrent configuration change and restart...
  ... Passed
Test: unreliable 1...
  ... Passed
Test: unreliable 2...
  ... Passed
Test: unreliable 3...
  ... Passed
Test: shard deletion (challenge 1) ...
  ... Passed
Test: unaffected shard access (challenge 2) ...
  ... Passed
Test: partial migration shard access (challenge 2) ...
  ... Passed
PASS
ok  	6.824/shardkv	101.503s
$
```

注意：你的服务器不应该调用分片控制器的Join()处理程序。测试器将在适当的时候调用Join()。

任务：你的第一个任务是通过第一个shardkv测试。在这个测试中，只有一个分片分配，所以你的代码应该非常类似于你的实验3服务器。最大的修改将是让你的服务器检测配置何时发生，并开始接受匹配它现在拥有的分片的键的请求。

现在你的解决方案适用于静态分片情况，是时候解决配置更改的问题了。你需要让你的服务器监视配置更改，当检测到一个时，开始分片迁移过程。如果副本组丢失分片，它必须立即停止对该分片中键的请求，并开始将该分片的数据迁移给接管所有权的副本组。如果副本组获得分片，它需要等待前一个所有者发送旧分片数据，然后才能接受该分片的请求。

任务：在配置更改期间实现分片迁移。确保副本组中的所有服务器在它们执行的操作序列中的同一点进行迁移，以便它们都接受或拒绝并发的客户端请求。你应该专注于通过第二个测试（"join then leave"），然后再处理后面的测试。当你通过直到但不包括TestDelete的所有测试时，你完成了这个任务。

注意：你的服务器需要定期轮询shardctrler以了解新配置。测试期望你的代码大约每100毫秒轮询一次；更频繁是可以的，但少得多可能会导致问题。

注意：服务器需要在配置更改期间相互发送RPC以传输分片。shardctrler的Config结构包含服务器名称，但你需要labrpc.ClientEnd来发送RPC。你应该使用传递给StartServer()的make_end()函数将服务器名称转换为ClientEnd。shardkv/client.go包含执行此操作的代码。

向server.go添加代码以定期从shardctrler获取最新配置，并添加代码在接收组不负责客户端键的分片时拒绝客户端请求。你应该仍然通过第一个测试。

如果服务器不负责客户端的键（即键的分片没有分配给服务器的组），你的服务器应该以ErrWrongGroup错误响应客户端RPC。确保你的Get、Put和Append处理程序在面对并发重新配置时正确做出此决定。

按顺序一次处理一个重新配置。

如果测试失败，检查gob错误（例如"gob: type not registered for interface ..."）。Go不认为gob错误是致命的，尽管它们对实验是致命的。

你需要为跨分片移动的客户端请求提供最多一次语义（重复检测）。

考虑shardkv客户端和服务器应该如何处理ErrWrongGroup。如果客户端收到ErrWrongGroup，它应该更改序列号吗？如果服务器在执行Get/Put请求时返回ErrWrongGroup，它应该更新客户端状态吗？

在服务器移动到新配置后，它继续存储它不再拥有的分片是可以接受的（尽管在真实系统中这将是可悲的）。这可能有助于简化你的服务器实现。

当组G1在配置更改期间需要来自G2的分片时，G2在处理日志条目过程中的什么点向G1发送分片重要吗？

你可以在RPC请求或回复中发送整个map，这可能有助于保持分片传输代码简单。

如果你的RPC处理程序在其回复中包含作为服务器状态一部分的map（例如键/值map），你可能会由于竞态而得到错误。RPC系统必须读取map才能将其发送给调用者，但它不持有覆盖map的锁。然而，当RPC系统读取它时，你的服务器可能会继续修改相同的map。解决方案是让RPC处理程序在回复中包含map的副本。

如果你将map或切片放在Raft日志条目中，你的键/值服务器随后在applyCh上看到条目并在键/值服务器的状态中保存对map/切片的引用，你可能会有竞态。制作map/切片的副本，并将副本存储在你的键/值服务器的状态中。竞态在你的键/值服务器修改map/切片和Raft在持久化其日志时读取它之间。

在配置更改期间，一对组可能需要在它们之间双向移动分片。如果你看到死锁，这是一个可能的来源。

无学分挑战练习
如果你要为生产使用构建这样的系统，这两个功能将是必不可少的。

状态垃圾收集
当副本组失去分片的所有权时，该副本组应该从其数据库中删除它丢失的键。保留它不再拥有并不再为其服务请求的值是浪费的。然而，这对迁移带来了一些问题。假设我们有两个组，G1和G2，有一个新配置C将分片S从G1移动到G2。如果G1在转换到C时从其数据库中删除S中的所有键，当G2尝试移动到C时如何获得S的数据？

挑战：使每个副本组保留旧分片的时间不超过绝对必要的时间。即使像上面G1这样的副本组中的所有服务器崩溃然后重新启动，你的解决方案也必须工作。如果你通过TestChallenge1Delete，你完成了这个挑战。

配置更改期间的客户端请求
处理配置更改的最简单方法是在转换完成之前禁止所有客户端操作。虽然概念上简单，但这种方法在生产级系统中不可行；它导致每当机器被带入或取出时所有客户端长时间暂停。更好的做法是继续服务不受正在进行的配置更改影响的分片。

挑战：修改你的解决方案，以便在配置更改期间，不受影响分片的键的客户端操作继续执行。当你通过TestChallenge2Unaffected时，你完成了这个挑战。

虽然上面的优化很好，但我们仍然可以做得更好。假设某个副本组G3，在转换到C时，需要来自G1的分片S1，和来自G2的分片S2。我们真的希望G3在收到必要状态后立即开始服务分片，即使它仍在等待其他分片。例如，如果G1宕机，G3应该仍然在从G2接收适当数据后立即开始为S2服务请求，尽管向C的转换尚未完成。

挑战：修改你的解决方案，使副本组在能够时立即开始服务分片，即使配置仍在进行中。当你通过TestChallenge2Partial时，你完成了这个挑战。

提交程序
重要：提交前，请最后一次运行所有测试。

另外，注意你的实验4分片服务器、实验4分片控制器和实验3 kvraft都必须使用相同的Raft实现。作为评分实验4的一部分，我们将重新运行实验2和实验3测试。

提交前，仔细检查你的解决方案适用于：

```shell
$ go test raft/...
$ go test kvraft/...
$ go test shardctrler/...
$ go test shardkv/...
```