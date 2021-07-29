package mr

import (
	"bufio"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io/ioutil"
	"log"
	"net/rpc"
	"os"
	"sort"
	"strconv"
	"sync"
)

// map 阶段应该将 Map 任务生成的中间件划分到 n 个 Reduce 中
// n 应该作为参数传递给 makemaster

var nReduce int
var mMap int

// KeyValue
// Map functions return a slice of KeyValue.
type KeyValue struct {
	Key   string
	Value string
}

// 通过 key 决定给那个 Reduce 任务执行
// use ihash(key) % NReduce to choose the reduce
// task number for each KeyValue emitted by Map.
//
func ihash(key string) int {
	h := fnv.New32a()
	if _, err := h.Write([]byte(key)); err != nil {
		log.Fatalf("Hash Error: %v", err)
	}
	return int(h.Sum32() & 0x7fffffff)
}

// Worker main/mrworker.go calls this function.
// 创建一个 Worker 进程，等待指令
// 通过 RPC 请求任务;等待机制，让 Worker 不断询问还是由 Master 通知
// 读文件并运行任务 - 得到的格式，任务类型和文件位置
// 使用encoding/json写读中间文件
// 完成任务通知 Master，任务完成状态和位置
func Worker(mapf func(string, string) []KeyValue,
	reducef func(string, []string) string) {
	for {
		rep := questTask()
		mMap = rep.MMap
		nReduce = rep.NReduce
		t := rep.Task
		switch t.TaskType {
		case MapPhase:
			f := rep.FileName
			MapTask(mapf, t, f)
		case ReducePhase:
			ReduceTask(reducef, t)
		case FinishedPhase:
			return
		default:
		}
		if rep := finishTask(t); rep.State == 0 {
			log.Printf("Work finished seccessfully, Task(%v, %v)", t.TaskType, t.Id)
		}
	}
}

func questTask() *GetTaskReply {
	args := Args{}
	reply := GetTaskReply{}
	call("Master.GetTaskHandler", &args, &reply)
	log.Printf("Get Task From Master: (%v, %d)", reply.Task.TaskType, reply.Task.Id)
	return &reply
}

func finishTask(task Task) *FinishTaskReply {
	args := Args{task}
	reply := FinishTaskReply{}
	call("Master.FinishTaskHandler", &args, &reply)
	return &reply
}

//
// send an RPC request to the master, wait for the response.
// usually returns true.
// returns false if something goes wrong.
//
func call(rpcname string, args interface{}, reply interface{}) bool {
	// c, err := rpc.DialHTTP("tcp", "127.0.0.1"+":1234")
	sockname := masterSock()
	c, err := rpc.DialHTTP("unix", sockname)
	if err != nil {
		log.Fatal("Dialing:", err)
	}
	defer c.Close()

	err = c.Call(rpcname, args, reply)
	if err == nil {
		return true
	}
	fmt.Println(err)
	return false
}

// MapTask 执行 Map 任务返回中间文件位置
func MapTask(mapf func(string, string) []KeyValue, task Task, fileName string) {
	file, err := os.Open(FilePath + fileName)
	defer func(fileName string) {
		if err := file.Close(); err != nil {
			log.Fatalf("Can't close file: %v", fileName)
		}
	}(fileName)
	if err != nil {
		log.Fatalf("Can't open file: %v", fileName)
	}
	content, err := ioutil.ReadAll(file)
	if err != nil {
		log.Fatalf("Can't read file: %v", fileName)
	}
	kva := mapf(fileName, string(content))
	tempFiles := createTempFiles(task, kva)
	// 只留下有内容的 Temp File
	if err := renameTempFiles(tempFiles, task.Id); err != nil {
		log.Fatalf("Can't rename temp files: %v", err)
	}
}

func createTempFiles(task Task, kva []KeyValue) map[int]*os.File {
	var wg sync.WaitGroup
	tempFiles := make(map[int]*os.File)
	for i := 0; i < nReduce; i++ {
		wg.Add(1)
		// task.Id 为已经创建的 map 任务编号，i 为 Reduce 任务编号
		fileName := "mr-" + strconv.Itoa(task.Id) + strconv.Itoa(i)
		f, err := ioutil.TempFile(TempFilePath, fileName)
		if err != nil {
			log.Fatalf("Create temp file fail: Task(%v, %v)", task.TaskType, task.Id)
		}
		tempFiles[i] = f
		wg.Done()
	}
	wg.Wait()
	for _, kv := range kva {
		reduceID := ihash(kv.Key) % nReduce
		tempFile := tempFiles[reduceID]
		content, _ := json.Marshal(kv)
		if _, err := tempFile.Write(content); err != nil {
			log.Fatal(err)
		}
		if _, err := tempFile.Write([]byte("\n")); err != nil {
			log.Fatal(err)
		}
	}
	return tempFiles
}

func renameTempFiles(tempFiles map[int]*os.File, taskID int) error {
	for id, f := range tempFiles {
		filename := "mr-" + strconv.Itoa(taskID) + "-" + strconv.Itoa(id)
		err := os.Rename(f.Name(), filename)
		if err != nil {
			return err
		}
	}
	return nil
}

type ByKey []KeyValue

// for sorting by key.
func (a ByKey) Len() int           { return len(a) }
func (a ByKey) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByKey) Less(i, j int) bool { return a[i].Key < a[j].Key }

// 执行 Reduce 任务
func ReduceTask(reducef func(string, []string) string, task Task) {
	temp := readTempFiles(task)
	sort.Sort(ByKey(temp))
	f := createTempOutputFile(task)
	defer func() {
		if err := f.Close(); err != nil {
			log.Fatalf("Can't close file: %v", f.Name())
		}
	}()
	for i := 0; i < len(temp); {
		j := i + 1
		for j < len(temp) && temp[j].Key == temp[i].Key {
			j++
		}
		var values []string
		for k := i; k < j; k++ {
			values = append(values, temp[k].Value)
		}
		res := reducef(temp[i].Key, values)
		if _, err := fmt.Fprintf(f, "%v %v\n", temp[i].Key, res); err != nil {
			log.Fatalf("Can't write file: %v", f.Name())
		}
		i = j
	}
	fileName := "mr-out-" + strconv.Itoa(task.Id)
	err := os.Rename(f.Name(), fileName)
	if err != nil {
		log.Fatal(err)
	}
}

func readTempFiles(task Task) []KeyValue {
	var temp []KeyValue
	for id := 0; id < mMap; id++ {
		// Id 为 Map 任务编号，task.Id 为已经创建的 Reduce 任务编号
		fileName := "mr-" + strconv.Itoa(id) + "-" + strconv.Itoa(task.Id)
		file, err := os.Open(TempFilePath + fileName)
		if err != nil {
			log.Fatalf("Can't open temp file: %v", fileName)
		}
		var kva []KeyValue
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			jsonStr := scanner.Text()
			kv := KeyValue{}
			err := json.Unmarshal([]byte(jsonStr), &kv)
			if err != nil {
				log.Fatalf("JSON transform error: %v", err)
			}
			kva = append(kva, kv)
		}
		temp = append(temp, kva...)
		if err := file.Close(); err != nil {
			log.Fatalf("Can't close file: %v", fileName)
		}
	}
	return temp
}

func createTempOutputFile(task Task) *os.File {
	filename := "mr-out-" + strconv.Itoa(task.Id)
	f, err := ioutil.TempFile(TempFilePath, filename)
	if err != nil {
		log.Fatalf("Can't create temp file: %v", filename)
	}
	return f
}
