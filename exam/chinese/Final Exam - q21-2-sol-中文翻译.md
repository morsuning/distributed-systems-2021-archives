# 6.824 2021 期末考试

考试时间为120分钟。请写下您所做的任何假设。
您被允许查阅6.824论文、笔记和实验代码，但除了参加考试和提问外不得使用互联网。
请在Piazza上以私密帖子或在讲座Zoom会议中以聊天方式向工作人员提问。请勿与任何人合作或讨论考试。

特别是，请勿在5月25日东部夏令时下午5:00之前与课程工作人员以外的任何人讨论考试内容。

## Lab3

Arthur Availability、Bea Byzantium和Cameron Communication都以不同方式在他们的Lab3 KVServer代码中实现了GET请求，但他们对哪个解决方案是正确的无法达成一致。

请阅读他们提出的三个实现。对于每个实现，说明它是否正确，并简要论证为什么正确或不正确。特别注意GET是否是线性化的。您可以假设快照功能被禁用。

对于这些实现中的每一个，PUT和APPEND都以相同的方式（正确）实现：

```go
func (kv *KVServer) PutAppend(args *PutAppendArgs, reply *PutAppendReply) {
    kv.mu.Lock()
    defer kv.mu.Unlock()

    newOp := Op{args.Op, args.Key, args.Value, args.ClientId, args.SequenceId}

    for !kv.killed() {
        index, term, ok := kv.rf.Start(newOp)
        if !ok {
            // 不是leader
            break
        }

        for kv.lastApplied < index && term == kv.rf.GetTerm() {
            kv.cond.Wait()
        }

        // 是我们的命令被执行了吗？
        client := kv.Clients[args.ClientId]
        if client.SequenceId == args.SequenceId {
            reply.Ok = true
            return
        }
    }

    reply.Ok = false
}
```

您可以假设当在apply通道上接收到日志条目或term改变时，会调用'kv.cond.Broadcast()'。

A. Arthur声称对键值存储所做的任何更改最终都会到达键值存储，只要系统在继续推进。因此，他按如下方式实现他的GET RPC处理器：

```go
func (kv *KVServer) Get(args *GetArgs, reply *GetReply) {
    kv.mu.Lock()
    defer kv.mu.Unlock()

    term := kv.rf.GetTerm()
    index := kv.lastApplied + 1

    for !kv.killed() && kv.rf.IsLeader() && term == kv.rf.GetTerm() {
        if kv.lastApplied >= index {
            reply.Ok = true
            reply.Result = kv.keyvalue[args.Key]
            return
        }

        kv.cond.Wait()
    }

    reply.Ok = false
}
```

他的GET实现是否正确？简要论证为什么正确或不正确。

|____|

B. Bea声称Arthur的解决方案是错误的，因为它没有在日志中提交任何内容。他们认为有必要为每个GET操作提交一个日志条目。

这是他们实现GET处理器的方式：

```go
func (kv *KVServer) Get(args *GetArgs, reply *GetReply) {
    newOp := Op{"Get", args.Key, "", args.ClientId, args.SequenceId}

    for !kv.killed() {
        index, term, ok := kv.rf.Start(newOp)
        if !ok {
            // 不是leader
            break
        }

        for kv.lastApplied < index && term == kv.rf.GetTerm() {
            kv.cond.Wait()
        }

        // 是我们的命令被执行了吗？
        client := kv.Clients[args.ClientId]
        if client.SequenceId == args.SequenceId {
            reply.Ok = true
            reply.Result = kv.keyvalue[args.Key]
            return
        }
    }

    reply.Ok = false
}
```

他们的GET实现是否正确？简要论证为什么正确或不正确。

|____|

C. Cameron对Bea的设计有问题。她声称他们的设计将线性化点放在了错误的位置；她反驳说应该在apply通道线程而不是RPC处理器线程中。

她修改apply通道线程来修复这个问题：

```go
func (kv *KVServer) perform(index int, op Op) {
    /* ... 其他代码 ... */

    if op.Opcode == "Get" {
        kv.Clients[op.ClientId] = &ClientState{
            SequenceId: op.SequenceId,
            LastResult: kv.keyvalue[op.Key],
        }
    }

    /* ... 其他情况 ... */
}
```

并修复Bea实现中所谓的错误：

```go
func (kv *KVServer) Get(args *GetArgs, reply *GetReply) {
    newOp := Op{"Get", args.Key, "", args.ClientId, args.SequenceId}

    for !kv.killed() {
        index, term, ok := kv.rf.Start(newOp)
        if !ok {
            // 不是leader
            break
        }

        for kv.lastApplied < index && term == kv.rf.GetTerm() {
            kv.cond.Wait()
        }

        // 是我们的命令被执行了吗？
        client := kv.Clients[args.ClientId]
        if client.SequenceId == args.SequenceId {
            reply.Ok = true
            reply.Result = client.LastResult
            return
        }
    }

    reply.Ok = false
}
```

她的GET实现是否正确？简要论证为什么正确或不正确。

|____|

## 链式复制

考虑van Renesse等人的论文"Chain replication for supporting high throughput and availability"。链式复制在链的头部执行更新，在链的尾部执行读取，同时提供强一致性。如果客户端和尾部在网络分区中与头部分离，链式复制如何保证客户端将读取最近完成写入的值？（简要解释您的答案。）

|____|


## Frangipani

考虑论文"Frangipani: a scalable distributed file system"。在Frangipani中，工作站将其所有文件系统修改写入Petal的日志后，将锁返回给锁服务。

A. 如果Frangipani先释放锁，然后写入日志并更新文件系统，会出现什么问题？（给出一个场景并简要解释您的答案。）

|____|


B. 工作站A可能在持有文件锁时崩溃。另一个工作站B如何获得该文件的锁并观察工作站可能进行的更新？（简要解释您的答案。）

|____|


## Spanner

考虑论文"Spanner: Google's Globally-Distributed Database"和以下包含3个事务的时间线：

```
  r/w T0 @  1: Wx1 C



                   |1-----------5| |E1-----9|
  r/w T1:               Wx2 C          CW



                                    |E2-------------L2|
  r/o T2:                                    Rx


                          ------------- time ----->
```

T0是一个读写事务，在时间戳1提交（C），向x写入1。T1是一个读写事务，向x写入2，在T0完成后开始。T2是一个只读事务，读取x，在T1完成后开始。

实时从左到右。[E...L]表示TrueTime返回的Earliest和Latest区间。

在C处提交T1时，协调器调用TT.now()，它返回[1,5]，如上所示。

A. T1将选择什么提交时间戳？（简要解释您的答案）

|____|

在CW处T1等待，重复调用TT.now()。

B. TT.now().earliest（E1）至少要达到什么值T1才能停止等待？（简要解释您的答案）

|____|

当T2读取x时，它使用TT.now().latest的值作为读取x的时间戳。

C. T2可能收到的最低读取时间戳是什么？（简要解释您的答案）

|____|

## FaRM

考虑FaRM论文"No compromises: distributed transactions with consistency, availability, and performance"的图4。

Zara Zoom通过将验证阶段与LOCK阶段合并来简化FaRM协议。她修改LOCK阶段以获取事务*读取*的所有对象的锁。她消除了验证阶段。

A. Zara的协议仍然正确吗？（简要解释您的答案）

|____|


B. Zara更改的缺点是什么？（简要解释您的答案并具体说明）

|____|


## Spark

考虑Zaharia等人的Spark论文"Resilient distributed datasets: a fault-tolerant abstraction for in-memory cluster computing"中的图3及其下面的代码。

Ben将以下行：

```go
val links = spark.textFile(...).map(...).persist()
```

改为：

```go
val links = spark.textFile(...).map(...)
```

A. Ben的程序仍然正确吗？（简要解释您的答案）

|____|


B. Ben更改的缺点是什么？（简要解释您的答案并具体说明）

|____|


## Memcache

Nishtala等人的论文"Scaling Memcache at Facebook"的4.3节描述了预热冷集群和为避免竞争条件而制定的2秒等待规则。该规则是客户端在冷集群中删除键后，任何客户端在2秒内都不能在冷集群中设置该键。

考虑一个区域，有一个存储集群和两个前端集群，一个冷集群和一个热集群。

Cara Cache提出了一个不同于2秒等待规则的计划，受"远程"标记技巧的启发。当客户端在冷集群中删除键时，客户端将该键标记为"已删除"。读取该键的第一个客户端照常获得租约并从数据库获取数据，而不是从热集群。然后客户端设置键（如果其租约仍然有效），移除"已删除"标记。

Cara的计划是否避免了4.3节讨论的竞争条件？（简要解释您的答案）

|____|


## Blockstack

您被聘用使用Blockstack编写一个简化版piazza应用程序的去中心化版本。应用程序只需要支持公共和私有的问答。它只需要支持1个班级（6.824），您可以假设应用程序知道6.824中每个学生和工作人员的姓名。

通过回答以下问题来概述一个设计，简要解释每个问题的答案。

A. 当用户编写问题或回复时，应用程序在哪里存储这些数据？

|____|


B. 应用程序如何确保私有问题保持私有？

|____|

C. 应用程序如何收集所有问题和答案以显示给其用户？

|____|

D. 用户如何确保找到另一个用户的正确公钥？

|____|

E. piazza.com在这个去中心化设计中扮演什么角色？

|____|




## 6.824

A. 在未来的6.824年度，我们应该省略哪些论文/讲座，因为它们没有用或太难理解？

[ ] 链式复制
[ ] Frangipani
[ ] 分布式事务
[ ] Spanner
[ ] FaRM
[ ] Spark
[ ] Memcache
[ ] SUNDR
[ ] Bitcoin
[ ] Blockstack
[ ] AnalogicFS

B. 哪些论文/讲座您发现最有用？

[ ] 链式复制
[ ] Frangipani
[ ] 分布式事务
[ ] Spanner
[ ] FaRM
[ ] Spark
[ ] Memcache
[ ] SUNDR
[ ] Bitcoin
[ ] Blockstack
[ ] AnalogicFS

C. 我们应该对课程进行哪些更改以使其更好？

|____|

----- 答案 -----

## Lab3

A. Arthur的解决方案是错误的。考虑当leader被分区到网络的少数部分，并且多数部分选举出新leader时会发生什么。少数leader可能继续用陈旧数据回答GET请求！

B. 这个问题有两个可接受的答案。
(1) Bea的解决方案是正确的。线性化点将在GET操作的提交点之后，但这没关系；重要的是线性化点既在GET调用之后又在GET返回之前。

(2) 第二个答案指出，必须提供的GET代码中获取锁，以使此解决方案避免竞争并正确（例如，不持有锁调用cond.Wait()会导致panic）。

C. 有两个可接受的答案。
(1) Cameron的解决方案是正确的（假设代码中的锁是正确的）。线性化点将是GET操作的提交点，这既在GET调用之后又在GET返回之前。

(2) 与(B)部分一样，应该在GET代码中获取锁以避免竞争和panic。

## 链式复制

一个正确的解释是指，一旦尾部被分区，CR将不应用更新，因为更新必须沿着完整的链复制，但这不会成功，因为尾部在单独的分区中。因此，任何进一步的更新都将被暂停，直到分区愈合或主节点更改配置以移除尾部。在此之前，尾部安全地提供查询服务——它拥有最新状态，并且状态不会改变。

一旦主节点更改配置并驱逐尾部，所有客户端和其他节点必须了解此更改。论文没有明确说明如何实现，但这涉及调度器（5.2节）和/或主节点等待直到尾部的租约到期（因此它不再提供请求）。

另一个正确的解释是指出客户端请求总是通过调度器，并且如果尾部与调度器在同一个分区中，那么尾部可以处理更新和读取。否则，系统将暂停，直到重新配置发生并且尾部从系统中移除。CR不会产生陈旧值。


## Frangipani

A. 此更改可能导致竞争条件。A释放对目录d的锁。A开始将更改应用到目录，同时B获取目录的锁。B读取不一致的目录，因为A尚未完成写入。B也可能将冲突的更改刷到Petal，因为它可以快速进行更改，工作站C可能请求锁，导致B在A仍在写入时写入Petal。

B. 锁有租约。一旦租约到期，锁服务器告诉B尝试恢复A的日志，以防A未完成操作就失败了。一旦B告诉锁服务器它已恢复A的日志，A的所有锁将被释放，B想要的锁将被授予。


## Spanner

A. 5。开始规则说不小于TT.now().latest的值（在问题设置中为5）。

B. 5+epsilon（如果限制为整数则为6），根据提交等待规则，这使事务等待直到5 < TT.now()。

C. 5+epsilon。T2在T1完成后开始（即在T1停止等待后），所以T2开始的绝对时间至少为5+epsilon。因此，TT.now().latest至少为该值。

## FaRM

A. 是的，它是正确的。获取锁仍然检查版本是否已更改以及锁是否已被获取。

B. 它降低了读取的性能，有几个原因。首先，只读事务必须序列化，而以前它们可以并行运行。其次，锁定需要远程CPU时间，而验证不需要远程CPU的任何工作。

## Spark

A. 是的，它是正确的

B. 它会很慢，因为links不会保存在内存中，循环的每次迭代都需要与links进行连接。

## Memcache

是的。第二个客户端会观察到键被标记为已删除，并将从数据库而不是热集群读取数据。如果其租约仍然有效，它将成功将键设置为新值。如果并发客户端对数据库中的键进行更新，其删除将使租约无效，客户端将无法设置现在陈旧的值。

## Blockcache

A. 每个参与者都有自己的云存储（S3、GDrive、AWS等），通过Gaia存储服务访问，他们在其中存储问题和答案。

B. 为了创建私有问题，学生使用讲师的公钥加密它们。讲师使用学生的公钥回复加密。两者还包括使用其公钥的加密副本，以便他们可以回读内容。由于存储是可变的和不可信的，完整性可以通过让每个用户用其私钥签署他们的问题/评论来提供。

C. 应用程序用Javascript编程，完全在每个用户的浏览器上运行，定期检查所有参与者的公共目录以获取最新的问题和答案。问题作为可变存储存储在存储层中，因此客户端将通过班级每个成员的zonefile检索相关的URI。客户端将使用其他班级成员的公钥验证签名的完整性，并使用用户的私钥解密私有帖子。

D. 区块链通过zone hash记录用户的姓名和间接的公钥。所有用户看到相同的区块链。

E. 没有。

## 6.824

省略：
CR                       XXXXX
Frangipani               XXXXXXXXXXXXXXXXX
分布式事务                XX
Spanner                  XXXXXXXXX
FaRM                     XXXXXXXXX
Spark                    XXXXX
Memcache                 XXXXXXXXX
SUNDR                    XXXXXXXXXXXXXXXXXXXXXXXXX
Bitcoin                  XXXX
Blockstack               XXXXXXXXXXXXXXXXXXXXXXX
AnalogicFS               XXXXXXXXXXXXXXXXXXXXXXXXX

有用：
CR                       XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
Frangipani               XXXXXXXXXXXXXXXXXX
分布式事务                XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
Spanner                  XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
FaRM                     XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
Spark                    XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
Memcache                 XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
SUNDR                    XXXXXXXXXXXXXXX
Bitcoin                  XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
Blockstack               XXXXXXXXXXXXXX
AnalogicFS               XXXXXXXXXXX

反馈：
助教考试复习会 XXXXXXX
发布lab 2解决方案以在其基础上构建 XXXXXX
较小的编程作业，专注于Raft之外的不同论文 XXXXX
不要ASCII笔记，发布带图表的幻灯片。XXXX
更多关于lab 4设计的细节/讲座 XXXX
lab 3的问答讲座 XXX
事先发布论文重要阅读点的笔记 XXX
在整个学期内分散论文而不是集中到最后 XXX
在更容易找到的地方发布lab技巧，保留Jose的漂亮打印笔记。XX
Linux文件系统主题（inodes）复习 X
更多办公时间/工作人员 X
为学生提供一致的机器/环境/VM评分器进行测试 X
论文作者或客座演讲中更多女性代表。X
拜占庭容错。X
包括课程后续材料的笔记 X
包括4B的设计文档，为破损代码提供部分学分。X
改为递减的迟交政策而不是D。X
更多关于Tor、Kademlia等系统而不是数据中心的阅读材料 X
每次讲座后给出讲座问题的答案。X
用更现代的论文如Ethereum替换SUNDR X
更多合作机会/使lab 4协作。X
降低考试权重 X
代码检查以获得反馈 X