package wxbot

import (
	"fmt"
)

type SyncWorkerPoolType chan chan SyncWorkRequest

var SyncWorkerPool SyncWorkerPoolType

func StartWorkDispatcher(qsize, nworkers int, bot *WxBot) {
	SyncWorkQueue = make(chan SyncWorkRequest, qsize)

	SyncWorkerPool = make(SyncWorkerPoolType, nworkers)

	fmt.Println("[INFO] start workers...")
	for i := 0; i < nworkers; i++ {
		worker := NewSyncWorker(i+1, SyncWorkerPool, bot)
		worker.Start()
	}

	go func() {
		for {
			select {
			case work := <-SyncWorkQueue:
				// if a work is arriving, dispatch the work to a worker
				go func() {
					worker := <-SyncWorkerPool
					worker <- work
				}()
			}
		}
	}()
}
