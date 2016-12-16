package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"wxbot"

	"github.com/gin-gonic/gin"
)

var bot *wxbot.WxBot

var crmMsgUrl string = "http://115.159.31.213:8701/myService/wx-msgs"

type MsgHandler struct{}

func (h MsgHandler) HandleMsg(bot *wxbot.WxBot, msg map[string]interface{}) error {
	if msg["msg_type_id"].(int) != 3 && msg["msg_type_id"].(int) != 4 {
		return nil
	}

	fmt.Printf("msg is %+v\n", msg)

	res, _, err := wxbot.MakeHttpRequest(wxbot.POST, crmMsgUrl, msg, nil)
	fmt.Println("[INFO] auto reply is", res)
	if err != nil {
		return err
	}

	var resMap map[string]interface{}
	if err := json.Unmarshal([]byte(res), &resMap); err != nil {
		fmt.Println("[ERROR]response could not be resolve")
		return err
	}

	autoReply, ok := resMap["auto_reply"]
	if !ok || strings.ToLower(autoReply.(string)) == "n" {
		return nil
	}

	replyContent := resMap["reply_content"].(map[string]interface{})
	toId := msg["from_user_id"].(string)

	if err := sendMsg(toId, replyContent); err != nil {
		fmt.Printf("[ERROR] failed to auto reply the message: %+v", err)
		return err
	}

	return nil
}

func sendMsg(toId string, content map[string]interface{}) error {
	msgType := content["type"].(float64)
	data := content["data"]
	switch msgType {
	case 0:
		return bot.SendMsgById(data.(string), toId)
	case 3:
		return bot.SendImgMsgById(data.(string), toId)
	}
	return nil
}

type autoSendMsg struct {
	AccountId string                 `json:"account_id"`
	ToUserId  string                 `json:"to_user_id"`
	Content   map[string]interface{} `json:"content"`
}

func autoSendMsgHandler(c *gin.Context) {
	var req autoSendMsg
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "参数不能解析"})
		c.AbortWithError(http.StatusBadRequest, errors.New("参数不能解析"))
		return
	}

	msgType, ok := req.Content["type"]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "content type不存在"})
		c.AbortWithError(http.StatusBadRequest, errors.New("content type不存在"))
		return
	}

	if reflect.ValueOf(msgType).Kind() != reflect.Float64 {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "content type需为整数"})
		c.AbortWithError(http.StatusBadRequest, errors.New("content type需为整数"))
		return
	}

	toId := req.ToUserId
	if err := sendMsg(toId, req.Content); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "消息发送失败"})
		c.AbortWithError(http.StatusInternalServerError, errors.New("消息发送失败"))
		return
	}

	c.JSON(http.StatusOK, map[string]interface{}{"success": "true"})
}

func main() {
	_crmMsgUrl := flag.String("crmMsgUrl", "http://115.159.31.213:8701/myService/wx-msgs", "infocrm处理转发的微信消息url")
	msgQueueSize := flag.String("msgQueueSize", "2048", "机器人处理收到消息同步任务队列大小, 默认值2048")
	qrcodeFormat := flag.String("qrcodeFormat", "tty", "微信登陆二维码显示形式(tty或者png), 默认值tty, tty表示二维码现在在屏幕终端上, png表示生成图片并用浏览器打开")
	syncWorkers := flag.String("syncWorkers", "128", "机器人处理同步收到消息worker的数目, 默认值128")
	apiServerPort := flag.Int("apiServerPort", 10010, "api server端口, 默认值10010")
	flag.Parse()

	crmMsgUrl = *_crmMsgUrl

	if *qrcodeFormat != "tty" && *qrcodeFormat != "png" {
		fmt.Println("[ERROR] qycodeFormat必须是tty或者png")
		os.Exit(1)
	}

	if _, err := strconv.Atoi(*msgQueueSize); err != nil {
		fmt.Println("[ERROR] msgQueueSize必须是正整数")
		os.Exit(1)
	}

	if _, err := strconv.Atoi(*syncWorkers); err != nil {
		fmt.Println("[ERROR] syncWorkeers必须是正整数")
		os.Exit(1)
	}

	go func() {
		gin.SetMode(gin.ReleaseMode)
		router := gin.Default()
		v1 := router.Group("/v1")
		v1.POST("/auto-send-msgs", autoSendMsgHandler)
		server := &http.Server{
			Addr:    fmt.Sprintf("%s:%d", "0.0.0.0", *apiServerPort),
			Handler: router,
		}
		server.ListenAndServe()
	}()

	msgHandler := MsgHandler{}
	options := map[string]string{
		"qrcode_format":  *qrcodeFormat,
		"msg_queue_size": *msgQueueSize,
		"sync_workers":   *syncWorkers,
	}
	bot = wxbot.NewWxBot(msgHandler, options)
	if err := bot.Run(); err != nil {
		os.Exit(1)
	}
}
