package wxbot

import (
	"fmt"
)

type SyncWorker struct {
	ID         int
	Work       chan SyncWorkRequest
	WorkerPool chan chan SyncWorkRequest
	Bot        *WxBot
}

func NewSyncWorker(id int, workerPool chan chan SyncWorkRequest, bot *WxBot) *SyncWorker {
	return &SyncWorker{
		ID:         id,
		WorkerPool: workerPool,
		Bot:        bot,
		Work:       make(chan SyncWorkRequest),
	}
}

func (w *SyncWorker) Start() {
	go func() {
		for {
			w.WorkerPool <- w.Work
			select {
			case work := <-w.Work:
				w.execute(work.Msg)
			}
		}
	}()
}

func (w *SyncWorker) execute(msg map[string]interface{}) {
	fmtMsg := w.Bot.formatMsg(msg)
	if fmtMsg == nil || len(fmtMsg) == 0 {
		return
	}

	if err := w.Bot.msgHandler.HandleMsg(w.Bot, fmtMsg); err != nil {
		fmt.Println("[WARN] failed to handle the msg", fmtMsg)
	}
}
