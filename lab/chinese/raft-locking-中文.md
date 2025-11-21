Raft 加锁建议

如果你想知道在 6.824 Raft 实验中如何使用锁，这里有一些规则和思考方式可能会有所帮助。

规则 1：当你有多个 goroutine 使用的数据，并且至少有一个 goroutine 可能修改这些数据时，这些 goroutine 应该使用锁来防止同时使用数据。Go 竞争检测器在检测违反此规则的行为方面相当不错（尽管它对下面的任何规则都没有帮助）。

规则 2：当代码对共享数据进行一系列修改时，如果其他 goroutine 在查看序列中途的数据时可能会出现故障，你应该在整个序列周围使用锁。

示例：

  rf.mu.Lock()
  rf.currentTerm += 1
  rf.state = Candidate
  rf.mu.Unlock()

如果另一个 goroutine 只看到这些更新中的一个（即旧状态与新任期，或者新任期与旧状态），那就是错误的。所以我们需要在整个更新序列中持续持有锁。所有其他使用 rf.currentTerm 或 rf.state 的代码也必须持有锁，以确保对所有使用进行独占访问。

Lock() 和 Unlock() 之间的代码通常被称为"临界区"。程序员选择的锁定规则（例如"goroutine 在使用 rf.currentTerm 或 rf.state 时必须持有 rf.mu"）通常被称为"锁定协议"。

规则 3：当代码对共享数据进行一系列读取（或读取和写入）时，如果另一个 goroutine 在序列中途修改数据会导致代码故障，你应该在整个序列周围使用锁。

一个可能在 Raft RPC 处理程序中出现的示例：

  rf.mu.Lock()
  if args.Term > rf.currentTerm {
   rf.currentTerm = args.Term
  }
  rf.mu.Unlock()

这段代码需要在整个序列中持续持有锁。Raft 要求 currentTerm 只能增加，永远不能减少。另一个 RPC 处理程序可能在单独的 goroutine 中执行；如果允许它在 if 语句和对 rf.currentTerm 的更新之间修改 rf.currentTerm，这段代码可能最终会减少 rf.currentTerm。因此，锁必须在整个序列中持续持有。此外，currentTerm 的每次其他使用都必须持有锁，以确保在我们的临界区期间没有其他 goroutine 修改 currentTerm。

真正的 Raft 代码需要比这些示例更长的临界区；例如，Raft RPC 处理程序可能应该在处理程序的整个过程中持有锁。

规则 4：在执行任何可能等待的操作时持有锁通常是个坏主意：读取 Go 通道、在通道上发送、等待定时器、调用 time.Sleep() 或发送 RPC（并等待回复）。一个原因是你可能希望其他 goroutine 在等待期间取得进展。另一个原因是避免死锁。想象两个对等方在持有锁的同时相互发送 RPC；两个 RPC 处理程序都需要接收方的锁；两个 RPC 处理程序都无法完成，因为它们需要等待 RPC 调用所持有的锁。

等待的代码应该首先释放锁。如果这不方便，有时创建一个单独的 goroutine 来执行等待会很有用。

规则 5：在锁的释放和重新获取之间对假设要小心。这可能在避免持有锁等待时出现。例如，这个发送投票 RPC 的代码是不正确的：

  rf.mu.Lock()
  rf.currentTerm += 1
  rf.state = Candidate
  for <每个对等方> {
    go func() {
      rf.mu.Lock()
      args.Term = rf.currentTerm
      rf.mu.Unlock()
      Call("Raft.RequestVote", &args, ...)
      // 处理回复...
    } ()
  }
  rf.mu.Unlock()

这段代码在单独的 goroutine 中发送每个 RPC。它是不正确的，因为 args.Term 可能与周围代码决定成为候选者时的 rf.currentTerm 不同。在周围代码创建 goroutine 和 goroutine 读取 rf.currentTerm 之间可能经过很多时间；例如，多个任期可能来来去去，对等方可能不再是候选者。解决这个问题的一种方法是让创建的 goroutine 使用周围代码持有锁时制作的 rf.currentTerm 副本。类似地，在 Call() 之后的回复处理代码必须在重新获取锁后重新检查所有相关假设；例如，它应该检查 rf.currentTerm 自从决定成为候选者以来是否没有改变。

解释和应用这些规则可能很困难。也许最令人困惑的是规则 2 和 3 中不应与其他 goroutine 的读取或写入交错的概念。如何识别这样的序列？应该如何决定序列应该在哪里开始和结束？

一种方法是从没有锁的代码开始，仔细思考需要在哪里添加锁以达到正确性。这种方法可能很困难，因为它需要推理并发代码的正确性。

更实际的方法从观察开始，如果没有并发（没有同时执行的 goroutine），你根本不需要锁。但是当 RPC 系统创建 goroutine 来执行 RPC 处理程序时，以及因为你需要在单独的 goroutine 中发送 RPC 以避免等待时，并发被强加给你。你可以通过识别 goroutine 开始的所有地方（RPC 处理程序、你在 Make() 中创建的后台 goroutine 等），在每个 goroutine 的最开始获取锁，并且只有在该 goroutine 完全完成并返回时才释放锁来有效地消除这种并发。这种锁定协议确保没有任何重要的东西并行执行；锁确保每个 goroutine 在任何其他 goroutine 被允许开始之前执行到完成。没有并行执行，很难违反规则 1、2、3 或 5。如果每个 goroutine 的代码在隔离中是正确的（当单独执行时，没有并发 goroutine），当你使用锁来抑制并发时，它可能仍然是正确的。所以你可以避免对正确性的显式推理，或显式识别临界区。

然而，规则 4 可能是个问题。所以下一步是找到代码等待的地方，并根据需要添加锁释放和重新获取（和/或 goroutine 创建），小心地在每次重新获取后重新建立假设。你可能会发现这个过程比直接识别需要锁以实现正确性的序列更容易。

（顺便说一句，这种方法牺牲的是通过多核上并行执行获得更好性能的任何机会：你的代码可能在不需要时持有锁，因此可能不必要地禁止 goroutine 的并行执行。另一方面，在单个 Raft 对等方内没有太多 CPU 并行性的机会。）