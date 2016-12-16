package wxbot

var SyncWorkQueue chan SyncWorkRequest

type SyncWorkRequest struct {
	Msg map[string]interface{}
}
