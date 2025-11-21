6.824 - 2021年春季
6.824 实验1：MapReduce
截止日期：2月26日周五 23:59ET（MIT时间）

简介
在本实验中，你将构建一个MapReduce系统。你将实现一个调用应用程序Map和Reduce函数并处理文件读写的worker进程，以及一个分配任务给worker并处理失败worker的coordinator进程。你将构建类似于MapReduce论文的系统。（注意：实验使用"coordinator"而不是论文中的"master"。）

开始准备
你需要为实验设置Go环境。

你将使用git（版本控制系统）获取初始实验软件。要了解更多关于git的信息，请查看Pro Git书籍或git用户手册。获取6.824实验软件：

```shell
$ git clone git://g.csail.mit.edu/6.824-golabs-2021 6.824
$ cd 6.824
$ ls
Makefile src
$
```

我们在src/main/mrsequential.go中提供了一个简单的顺序mapreduce实现。它在单个进程中一次运行一个map和reduce。我们还提供了几个MapReduce应用程序：mrapps/wc.go中的单词计数，和mrapps/indexer.go中的文本索引器。你可以按如下方式顺序运行单词计数：

```shell
$ cd ~/6.824
$ cd src/main
$ go build -race -buildmode=plugin ../mrapps/wc.go
$ rm mr-out*
$ go run -race mrsequential.go wc.so pg*.txt
$ more mr-out-0
A 509
ABOUT 2
ACT 8
...
```

（注意：如果不使用-race编译，将无法使用-race运行）
mrsequential.go将其输出保存在文件mr-out-0中。输入来自名为pg-xxx.txt的文本文件。

你可以随意借用mrsequential.go中的代码。你还应该查看mrapps/wc.go，了解MapReduce应用程序代码的样子。

你的任务（中等/困难）
你的任务是实现一个分布式MapReduce，包含两个程序，coordinator和worker。将只有一个coordinator进程，和一个或多个并行执行的worker进程。在真实系统中，worker会在一堆不同的机器上运行，但在这个实验中，你将在单台机器上运行它们。worker通过RPC与coordinator通信。每个worker进程向coordinator请求任务，从一个或多个文件读取任务的输入，执行任务，并将任务的输出写入一个或多个文件。coordinator应该注意到worker没有在合理的时间内完成任务（在这个实验中，使用十秒），并将相同的任务分配给不同的worker。

我们为你提供了一些起始代码。coordinator和worker的"main"程序在main/mrcoordinator.go和main/mrworker.go中；不要更改这些文件。你应该将你的实现放在mr/coordinator.go、mr/worker.go和mr/rpc.go中。

以下是在单词计数MapReduce应用程序上运行代码的方法。首先，确保单词计数插件是最新构建的：

```shell
$ go build -race -buildmode=plugin ../mrapps/wc.go
```

在主目录中，运行coordinator。

```shell
$ rm mr-out*
$ go run -race mrcoordinator.go pg-*.txt
```

mrcoordinator.go的pg-*.txt参数是输入文件；每个文件对应一个"split"，是一个Map任务的输入。-race标志启用go的竞态检测器。

在一个或多个其他窗口中，运行一些worker：

```shell
$ go run -race mrworker.go wc.so
```

当worker和coordinator完成后，查看mr-out-*中的输出。完成实验后，输出文件的排序并集应该与顺序输出匹配，如下所示：

```shell
$ cat mr-out-* | sort | more
A 509
ABOUT 2
ACT 8
...
```

我们在main/test-mr.sh中提供了一个测试脚本。测试检查wc和indexer MapReduce应用程序在给定pg-xxx.txt文件作为输入时是否产生正确的输出。测试还检查你的实现是否并行运行Map和Reduce任务，以及你的实现是否从运行任务时崩溃的worker中恢复。

如果你现在运行测试脚本，它会挂起，因为coordinator永远不会完成：

```shell
$ cd ~/6.824/src/main
$ bash test-mr.sh
*** Starting wc test.
```

你可以将mr/coordinator.go中Done函数的ret := false更改为true，使coordinator立即退出。然后：

```shell
$ bash test-mr.sh
*** Starting wc test.
sort: No such file or directory
cmp: EOF on mr-wc-all
--- wc output is not the same as mr-correct-wc.txt
--- wc test: FAIL
$
```

测试脚本期望在名为mr-out-X的文件中看到输出，每个reduce任务一个文件。mr/coordinator.go和mr/worker.go的空实现不产生这些文件（或做太多其他事情），所以测试失败。

当你完成后，测试脚本输出应该如下所示：

```shell
$ bash test-mr.sh
*** Starting wc test.
--- wc test: PASS
*** Starting indexer test.
--- indexer test: PASS
*** Starting map parallelism test.
--- map parallelism test: PASS
*** Starting reduce parallelism test.
--- reduce parallelism test: PASS
*** Starting crash test.
--- crash test: PASS
*** PASSED ALL TESTS
$
```

你还会看到一些来自Go RPC包的错误，看起来像这样：

2019/12/16 13:27:09 rpc.Register: method "Done" has 1 input parameters; needs exactly three

忽略这些消息；将coordinator注册为RPC服务器会检查其所有方法是否适合RPC（有3个输入）；我们知道Done不是通过RPC调用的。

一些规则：
- map阶段应该将中间键分成nReduce个reduce任务的桶，其中nReduce是main/mrcoordinator.go传递给MakeCoordinator()的参数。
- worker实现应该将第X个reduce任务的输出放在文件mr-out-X中。
- mr-out-X文件应该每行包含一个Reduce函数输出。行应该用Go的"%v %v"格式生成，使用键和值调用。查看main/mrsequential.go中注释为"this is the correct format"的行。如果你的实现偏离此格式太多，测试脚本将失败。
- 你可以修改mr/worker.go、mr/coordinator.go和mr/rpc.go。你可以临时修改其他文件进行测试，但确保你的代码与原始版本一起工作；我们将用原始版本进行测试。
- worker应该将中间Map输出放在当前目录的文件中，你的worker可以在以后读取它们作为Reduce任务的输入。
- main/mrcoordinator.go期望mr/coordinator.go实现一个Done()方法，当MapReduce作业完全完成时返回true；那时，mrcoordinator.go将退出。
- 当作业完全完成时，worker进程应该退出。一个简单的实现方法是使用call()的返回值：如果worker无法联系coordinator，它可以假设coordinator因为作业完成而退出，所以worker也可以终止。根据你的设计，你可能会发现coordinator可以给worker一个"请退出"的伪任务也很有帮助。

提示
开始的一个方法是修改mr/worker.go的Worker()发送RPC到coordinator请求任务。然后修改coordinator以响应尚未启动的map任务的文件名。然后修改worker读取该文件并调用应用程序Map函数，如mrsequential.go中那样。

应用程序Map和Reduce函数在运行时使用Go插件包加载，从名称以.so结尾的文件中加载。

如果你更改了mr/目录中的任何内容，你可能必须重新构建你使用的任何MapReduce插件，使用类似go build -race -buildmode=plugin ../mrapps/wc.go的命令。

这个实验依赖于worker共享文件系统。当所有worker在同一台机器上运行时这很简单，但如果worker在不同机器上运行，则需要像GFS这样的全局文件系统。

中间文件的合理命名约定是mr-X-Y，其中X是Map任务号，Y是reduce任务号。

worker的map任务代码需要一种方法将中间键/值对存储在文件中，以便在reduce任务期间可以正确读回。一种可能性是使用Go的encoding/json包。将键/值对写入JSON文件：

```go
enc := json.NewEncoder(file)
for _, kv := ... {
  err := enc.Encode(&kv)
```

并读回这样的文件：

```go
dec := json.NewDecoder(file)
for {
  var kv KeyValue
  if err := dec.Decode(&kv); err != nil {
    break
  }
  kva = append(kva, kv)
}
```

你worker的map部分可以使用ihash(key)函数（在worker.go中）为给定键选择reduce任务。

你可以从mrsequential.go中借用一些代码来读取Map输入文件，在Map和Reduce之间排序中间键/值对，以及将Reduce输出存储在文件中。

coordinator作为RPC服务器将是并发的；不要忘记锁定共享数据。

使用Go的竞态检测器，使用go build -race和go run -race。test-mr.sh默认使用竞态检测器运行测试。

worker有时需要等待，例如reduces在最后一个map完成之前无法开始。一种可能性是worker定期向coordinator请求工作，在每个请求之间使用time.Sleep()睡眠。另一种可能性是coordinator中的相关RPC处理程序有一个等待循环，使用time.Sleep()或sync.Cond。Go为每个RPC在其自己的线程中运行处理程序，所以一个处理程序在等待不会阻止coordinator处理其他RPC。

coordinator无法可靠地区分崩溃的worker、活着但因某些原因停滞的worker，以及执行但太慢而无用的worker。你能做的最好的事情是让coordinator等待一段时间，然后放弃并将任务重新分配给不同的worker。对于这个实验，让coordinator等待十秒；之后coordinator应该假设worker已经死了（当然，它可能没有）。

如果你选择实现备份任务（第3.6节），请注意我们测试你的代码在worker执行任务而不崩溃时不会安排多余的任务。备份任务只应该在相对较长的时间后（例如10秒）安排。

要测试崩溃恢复，你可以使用mrapps/crash.go应用程序插件。它在Map和Reduce函数中随机退出。

为了确保在崩溃情况下没有人观察到部分写入的文件，MapReduce论文提到了使用临时文件并在完全写入后原子重命名的技巧。你可以使用ioutil.TempFile创建临时文件，使用os.Rename原子重命名它。

test-mr.sh在子目录mr-tmp中运行所有进程，所以如果出现错误，你想查看中间或输出文件，请查看那里。你可以修改test-mr.sh在失败测试后退出，这样脚本不会继续测试（并覆盖输出文件）。

test-mr-many.sh提供了一个基本的脚本，用于超时运行test-mr.sh（我们将这样测试你的代码）。它接受运行测试的次数作为参数。你不应该并行运行几个test-mr.sh实例，因为coordinator将重用相同的套接字，导致冲突。

无学分挑战练习
挑战：实现你自己的MapReduce应用程序（参见mrapps/*中的示例），例如分布式Grep（MapReduce论文第2.3节）。

挑战：让你的MapReduce coordinator和worker在不同的机器上运行，就像它们在实践中那样。你需要设置RPC通过TCP/IP而不是Unix套接字通信（参见Coordinator.server()中的注释行），并使用共享文件系统读/写文件。例如，你可以ssh到MIT的多个Athena集群机器，它们使用AFS共享文件；或者你可以租用几个AWS实例并使用S3进行存储。