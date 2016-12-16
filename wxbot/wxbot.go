package wxbot

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"
	"github.com/toqueteos/webbrowser"
)

const (
	getUuidUrl   = "https://login.weixin.qq.com/jslogin"
	genQrCodeUrl = "https://login.weixin.qq.com/l/"
	loginUrl     = "https://login.weixin.qq.com/cgi-bin/mmwebwx-bin/login"
	appId        = "wx782c26e4c19acffb"
)

type Contact struct {
	Uin              int                      `json:"Uin"`
	UserName         string                   `json:"UserName"`
	NickName         string                   `json:"NickName"`
	HeadImgUrl       string                   `json:"HeadImgUrl"`
	ContactFlag      int                      `json:"ContactFlag"`
	MemberCount      int                      `json:"MemberCount"`
	MemberList       []map[string]interface{} `json:"MemberList"`
	RemarkName       string                   `json:"RemarkName"`
	HideInputBarFlag int                      `json:"HideInputBarFlag"`
	Sex              int                      `json:"Sex"`
	Signature        string                   `json:"Signature"`
	VerifyFlag       int                      `json:"VerifyFlag"`
	OwnerUin         int                      `json:"OwnerUin"`
	PYInitial        string                   `json:"PYInitial"`
	PYQuanPin        string                   `json:"PYQuanPin"`
	RemarkPYInitial  string                   `json:"RemarkPYInitial"`
	RemarkPYQuanPin  string                   `json:"RemarkPYQuanPin"`
	StarFriend       int                      `json:"StarFriend"`
	AppAccountFlag   int                      `json:"AppAccountFlag"`
	Statues          int                      `json:"Statues"`
	AttrStatus       float64                  `json:"AttrStatus"`
	Province         string                   `json:"Province"`
	City             string                   `json:"City"`
	Alias            string                   `json:"Alias"`
	SnsFlag          int                      `json:"SnsFlag"`
	UniFriend        int                      `json:"UniFriend"`
	DisplayName      string                   `json:"DisplayName"`
	ChatRoomId       int                      `json:"ChatRoomId"`
	KeyWord          string                   `json:"KeyWord"`
	EncryChatRoomId  string                   `json:"EncryChatRoomId"`
}

type Member struct {
	Type string  `json:"type"`
	Info Contact `json:"contact"`
}
type MsgHandler interface {
	HandleMsg(bot *WxBot, msg map[string]interface{}) error
}

type WxBot struct {
	msgHandler   MsgHandler
	uuid         string
	dataDir      string
	qrcodeFormat string
	redirectUri  string
	baseUri      string
	passTicket   string
	skey         string
	wxuin        string
	wxsid        string
	deviceId     string
	baseRequest  map[string]interface{}
	myAccount    map[string]interface{}
	syncKey      map[string]interface{}
	syncKeyStr   string
	publicList   []Contact
	contactList  []Contact
	specialList  []Contact
	groupList    []Contact
	syncHost     string

	accountInfo      map[string]map[string]Member
	encryChatRoomIds map[string]string
	groupMembers     map[string][]Contact

	sync.RWMutex

	msgQueueSize int
	syncWorkers  int
	cookie       *cookiejar.Jar
}

func NewWxBot(msgHandler MsgHandler, options map[string]string) *WxBot {
	dataDir, err := os.Getwd()
	if err != nil {
		return nil
	}
	if _dataDir, ok := options["data_dir"]; ok {
		dataDir = _dataDir
	}

	qrcodeFormat := "png"
	if _qrcodeFormat, ok := options["qrcode_format"]; ok {
		qrcodeFormat = _qrcodeFormat
	}

	deviceId := "e" + (strconv.FormatFloat(rand.Float64(), 'f', -1, 64))[2:17]

	msgQueueSize := 2048
	syncWorkers := 128

	if _msgQueueSize, ok := options["msg_queue_size"]; ok {
		_qsize, err := strconv.Atoi(_msgQueueSize)
		if err != nil {
			return nil
		}

		if _qsize > 0 {
			msgQueueSize = _qsize
		}
	}

	if _syncWorkers, ok := options["sync_workers"]; ok {
		_workers, err := strconv.Atoi(_syncWorkers)
		if err != nil {
			return nil
		}

		if _workers > 0 {
			syncWorkers = _workers
		}
	}

	cookie, _ := cookiejar.New(nil)
	return &WxBot{
		msgHandler:   msgHandler,
		dataDir:      dataDir,
		qrcodeFormat: qrcodeFormat,
		deviceId:     deviceId,
		accountInfo: map[string]map[string]Member{
			"group_member":  make(map[string]Member),
			"normal_member": make(map[string]Member),
		},
		msgQueueSize: msgQueueSize,
		syncWorkers:  syncWorkers,
		cookie:       cookie,
	}
}

func (bot *WxBot) getUuid() error {
	params := map[string]interface{}{
		"appid": appId,
		"fun":   "new",
		"lang":  "zh_CN",
		"_":     time.Now().Unix(),
	}

	res, _, err := MakeHttpRequest(GET, getUuidUrl, params, bot.cookie)
	if err != nil {
		fmt.Println(err)
		return err
	}

	re := regexp.MustCompile(`window.QRLogin.code = (\d+); window.QRLogin.uuid = "(\S+?)"`)
	matches := re.FindStringSubmatch(res)
	code := matches[1]
	bot.uuid = matches[2]

	if code != "200" {
		return errors.New("Failed to get the uuid")
	}

	return nil
}

func (bot *WxBot) genQrCode() error {
	url := genQrCodeUrl + bot.uuid
	if bot.qrcodeFormat == "png" {
		imgPath := bot.dataDir + "/qr.png"
		if err := qrcode.WriteFile(url, qrcode.Medium, 256, imgPath); err != nil {
			return err
		}
		webbrowser.Open("file://" + imgPath)
	} else if bot.qrcodeFormat == "tty" {
		code, err := generateQrcode2Terminal(url)
		if err != nil {
			return err
		}
		fmt.Println(code)
	}
	return nil
}

func (bot *WxBot) wait4login() error {
	tip := 1
	retryTimes := 5
	for i := 1; i <= retryTimes; i++ {
		params := map[string]interface{}{
			"tip":  tip,
			"uuid": bot.uuid,
			"_":    time.Now().Unix(),
		}

		res, code, _ := MakeHttpRequest(GET, loginUrl, params, bot.cookie)
		switch code {
		case 200:
			re := regexp.MustCompile(`window.code=(\d+);`)
			matches := re.FindStringSubmatch(res)

			code := matches[1]

			if code == "201" {
				fmt.Println("[INFO] 请在手机上确认登陆..")
				tip = 0
			} else if code == "200" {
				re = regexp.MustCompile(`window.redirect_uri="(\S+?)";`)
				matches = re.FindStringSubmatch(res)
				bot.redirectUri = matches[1] + "&fun=new"
				bot.baseUri = bot.redirectUri[:strings.LastIndex(bot.redirectUri, "/")]
				return nil
			} else if code == "408" {
				fmt.Println("[Warn] 登陆超时. 请1秒后重新扫码登陆...")
				tip = 1
				time.Sleep(1 * time.Second)
			}
		default:
			fmt.Println("[Error] WeChat login exception, retry in 1 secs later...")
			tip = 1
			time.Sleep(1 * time.Second)
		}
	}

	return errors.New("failed to login")
}

func (bot *WxBot) login() error {
	if len(bot.redirectUri) < 4 {
		return errors.New("redirect uri is invalid")
	}
	res, _, err := MakeHttpRequest(GET, bot.redirectUri, nil, bot.cookie)
	if err != nil {
		return err
	}

	type Error struct {
		PassTicket string `xml:"pass_ticket"`
		Skey       string `xml:"skey"`
		Wxuin      string `xml:"wxuin"`
		Wxsid      string `xml:"wxsid"`
	}
	_res := &Error{}
	if err := xml.Unmarshal([]byte(res), _res); err != nil {
		return err
	}

	if _res.PassTicket == "" || _res.Skey == "" || _res.Wxuin == "" || _res.Wxsid == "" {
		return errors.New("base identifier certificate was not got correctly")
	}

	bot.passTicket = _res.PassTicket
	bot.skey = _res.Skey
	bot.wxuin = _res.Wxuin
	bot.wxsid = _res.Wxsid

	bot.baseRequest = map[string]interface{}{
		"Uin":      bot.wxuin,
		"Sid":      bot.wxsid,
		"Skey":     bot.skey,
		"DeviceID": bot.deviceId,
	}

	return nil
}

func (bot *WxBot) init() error {
	url := fmt.Sprintf("%s/webwxinit?r=%d&lang=en_US&pass_ticket=%s", bot.baseUri, time.Now().Unix(), bot.passTicket)
	data := map[string]interface{}{
		"BaseRequest": bot.baseRequest,
	}

	res, _, err := MakeHttpRequest(POST, url, data, bot.cookie)
	if err != nil {
		return err
	}

	var resMap map[string]interface{}
	if err := json.Unmarshal([]byte(res), &resMap); err != nil {
		return err
	}

	if err := bot.isBadResponse(resMap); err != nil {
		return err
	}

	bot.myAccount = resMap["User"].(map[string]interface{})
	bot.syncKey = resMap["SyncKey"].(map[string]interface{})

	keyList := bot.syncKey["List"].([]interface{})
	keyListArr := make([]string, len(keyList))
	fmtKeyList := make([]map[string]interface{}, len(keyList))
	for index, key := range keyList {
		_key := key.(map[string]interface{})
		_v := strconv.FormatFloat(_key["Val"].(float64), 'f', -1, 64)

		keyListArr[index] = fmt.Sprintf("%v_%s", _key["Key"], _v)
		_cv, _ := strconv.ParseInt(_v, 10, 64)
		fmtKeyList[index] = map[string]interface{}{"Key": _key["Key"], "Val": _cv}
	}
	bot.syncKeyStr = strings.Join(keyListArr, "|")
	bot.syncKey["List"] = fmtKeyList

	return nil
}

func (bot *WxBot) statusNotify() error {
	url := fmt.Sprintf("%s/webwxstatusnotify?lang=zh_CN&pass_ticket=%s", bot.baseUri, bot.passTicket)
	cUin, _ := strconv.ParseInt(bot.baseRequest["Uin"].(string), 10, 64)
	bot.baseRequest["Uin"] = cUin
	data := map[string]interface{}{
		"BaseRequest":  bot.baseRequest,
		"Code":         3,
		"FromUserName": bot.myAccount["UserName"],
		"ToUserName":   bot.myAccount["UserName"],
		"ClientMsgId":  time.Now().Unix(),
	}

	res, _, err := MakeHttpRequest(POST, url, data, bot.cookie)
	if err != nil {
		return err
	}

	var resMap map[string]interface{}
	if err := json.Unmarshal([]byte(res), &resMap); err != nil {
		return err
	}

	if err := bot.isBadResponse(resMap); err != nil {
		return err
	}

	return nil
}

func (bot *WxBot) batchGetGroupMembers() error {
	if len(bot.groupList) == 0 {
		return nil
	}

	url := fmt.Sprintf("%s/webwxbatchgetcontact?type=ex&r=%s&pass_ticket=%s", bot.baseUri, time.Now().Unix(), bot.passTicket)
	dataList := make([]map[string]interface{}, len(bot.groupList))
	for index, group := range bot.groupList {
		dataList[index] = map[string]interface{}{"UserName": group.UserName, "EncryChatRoomId": ""}
	}
	data := map[string]interface{}{
		"BaseRequest": bot.baseRequest,
		"Count":       len(bot.groupList),
		"List":        dataList,
	}
	res, _, err := MakeHttpRequest(POST, url, data, bot.cookie)
	if err != nil {
		return err
	}

	var resMap map[string]interface{}
	if err := json.Unmarshal([]byte(res), &resMap); err != nil {
		return err
	}

	if err := bot.isBadResponse(resMap); err != nil {
		return err
	}

	contactListObj, ok := resMap["ContactList"]
	if !ok {
		return nil
	}
	contactList := contactListObj.([]interface{})

	groupMembers := make(map[string][]Contact)
	encryChatRoomIds := make(map[string]string)
	for _, group := range contactList {
		_group := group.(map[string]interface{})
		gid := _group["UserName"]
		members := _group["MemberList"]
		membersBytes, _ := json.Marshal(members)
		var _members []Contact
		json.Unmarshal(membersBytes, &_members)
		groupMembers[gid.(string)] = _members
		encryChatRoomIds[gid.(string)] = _group["EncryChatRoomId"].(string)
	}

	bot.groupMembers = groupMembers
	bot.encryChatRoomIds = encryChatRoomIds

	return nil
}

func (bot *WxBot) getContact() error {
	bot.Lock()
	defer bot.Unlock()

	specialUser := []string{
		"newsapp", "fmessage", "filehelper", "weibo", "qqmail",
		"fmessage", "tmessage", "qmessage", "qqsync", "floatbottle",
		"lbsapp", "shakeapp", "medianote", "qqfriend", "readerapp",
		"blogapp", "facebookapp", "masssendapp", "meishiapp",
		"feedsapp", "voip", "blogappweixin", "weixin", "brandsessionholder",
		"weixinreminder", "wxid_novlwrv3lqwv11", "gh_22b87fa7cb3c",
		"officialaccounts", "notification_messages", "wxid_novlwrv3lqwv11",
		"gh_22b87fa7cb3c", "wxitil", "userexperience_alarm",
	}

	bot.contactList = make([]Contact, 0)
	bot.publicList = make([]Contact, 0)
	bot.specialList = make([]Contact, 0)
	bot.groupList = make([]Contact, 0)

	url := fmt.Sprintf("%s/webwxgetcontact?pass_ticket=%s&skey=%s&r=%d", bot.baseUri, bot.passTicket, bot.skey, time.Now().Unix())
	data := map[string]interface{}{
		"BaseRequest": bot.baseRequest,
	}

	res, _, err := MakeHttpRequest(POST, url, data, bot.cookie)
	if err != nil {
		return err
	}

	var resMap map[string]interface{}
	if err := json.Unmarshal([]byte(res), &resMap); err != nil {
		return err
	}

	if err := bot.isBadResponse(resMap); err != nil {
		return err
	}

	memberListObj, ok := resMap["MemberList"]
	if !ok {
		return nil
	}

	memberListBytes, err := json.Marshal(memberListObj)
	if err != nil {
		return err
	}

	var memberList []Contact
	if err := json.Unmarshal(memberListBytes, &memberList); err != nil {
		return err
	}

	for _, contact := range memberList {
		member := Member{Type: "public", Info: contact}
		if contact.VerifyFlag&8 != 0 {
			bot.publicList = append(bot.publicList, contact)
		} else if Contains(contact.UserName, specialUser) {
			bot.specialList = append(bot.specialList, contact)
			member.Type = "special"
		} else if strings.Index(contact.UserName, "@@") != -1 {
			bot.groupList = append(bot.groupList, contact)
			member.Type = "group"
		} else if contact.UserName == bot.myAccount["UserName"].(string) {
			member.Type = "self"
		} else {
			bot.contactList = append(bot.contactList, contact)
		}
		bot.accountInfo["normal_member"][contact.UserName] = member
	}

	if err := bot.batchGetGroupMembers(); err != nil {
		return err
	}

	return nil

}

func (bot *WxBot) testSyncCheck() error {
	//bot.syncHost = "webpush"
	//return nil
	maybeHosts := []string{"webpush", "webpush2"}
	for _, host := range maybeHosts {
		bot.syncHost = host
		for i := 0; i < 5; i++ {
			if code, _, err := bot.syncCheck(); err == nil && code == "0" {
				fmt.Println("sync code is ", code)
				return nil
			} else {
				fmt.Printf("[ERROR] test sync check failed err %+v, code: %s\n", err, code)
			}
			time.Sleep(time.Second)
		}
	}
	return errors.New("no valid sync host is available")
}

func (bot *WxBot) syncCheck() (string, string, error) {
	params := map[string]interface{}{
		"r": time.Now().Unix(),
		//"r":        1476176582226,
		"sid":      bot.wxsid,
		"uin":      bot.wxuin,
		"skey":     bot.skey,
		"deviceid": bot.deviceId,
		//"synckey":     bot.syncKeyStr,
		"pass_ticket": bot.passTicket,
		"_":           time.Now().Unix(),
		//"_": 1476176582226,
	}

	url := fmt.Sprintf("https://%s.weixin.qq.com/cgi-bin/mmwebwx-bin/synccheck", bot.syncHost)
	//res, _, err := MakeHttpRequest(GET, url, params, bot.cookie)
	res, _, err := MakeHttpRequest(GET, url, params, nil)
	if err != nil {
		return "", "", err
	}

	re := regexp.MustCompile(`window.synccheck=\{retcode:"(\d+)",selector:"(\d+)"\}`)
	matches := re.FindStringSubmatch(res)
	code := matches[1]
	selector := matches[2]
	return code, selector, nil
}

func (bot *WxBot) sync() ([]map[string]interface{}, error) {
	url := fmt.Sprintf("%s/webwxsync?sid=%s&skey=%s&lang=en_US&pass_ticket=%s", bot.baseUri, bot.wxsid, bot.skey, bot.passTicket)
	data := map[string]interface{}{
		"BaseRequest": bot.baseRequest,
		"SyncKey":     bot.syncKey,
		"rr":          ^(time.Now().Unix()),
	}

	res, _, err := MakeHttpRequest(POST, url, data, bot.cookie)
	if err != nil {
		return nil, err
	}

	var resMap map[string]interface{}
	if err := json.Unmarshal([]byte(res), &resMap); err != nil {
		return nil, err
	}

	if err := bot.isBadResponse(resMap); err != nil {
		return nil, err
	}

	bot.syncKey = resMap["SyncKey"].(map[string]interface{})
	keyList := bot.syncKey["List"].([]interface{})
	keyListArr := make([]string, len(keyList))
	fmtKeyList := make([]map[string]interface{}, len(keyList))
	for index, key := range keyList {
		_key := key.(map[string]interface{})
		_v := strconv.FormatFloat(_key["Val"].(float64), 'f', -1, 64)

		keyListArr[index] = fmt.Sprintf("%v_%s", _key["Key"], _v)
		_cv, _ := strconv.ParseInt(_v, 10, 64)
		fmtKeyList[index] = map[string]interface{}{"Key": _key["Key"], "Val": _cv}
	}
	bot.syncKeyStr = strings.Join(keyListArr, "|")
	bot.syncKey["List"] = fmtKeyList
	msgList, ok := resMap["AddMsgList"]
	if !ok {
		return nil, nil
	}

	msgListBytes, err := json.Marshal(msgList)
	if err != nil {
		return nil, err
	}

	var msgs []map[string]interface{}
	if err := json.Unmarshal(msgListBytes, &msgs); err != nil {
		return nil, err
	}

	return msgs, nil
}

func (bot *WxBot) syncMsg(qsize, nworkers int) error {
	if err := bot.testSyncCheck(); err != nil {
		return err
	}

	fmt.Printf("[INFO]同步主机为%s\n", bot.syncHost)

	StartWorkDispatcher(qsize, nworkers, bot)

	for {
		code, selector, err := bot.syncCheck()
		if err == nil {
			if code == "1100" || code == "1101" {
				fmt.Println("[INFO] 微信账号在其他地方登陆了...")
				break
			}

			if code != "0" {
				fmt.Println("[ERROR] 消息同步失败...")
			}

			if selector == "0" {
				fmt.Println("[INFO] 没有需要同步的消息")
			}

			if selector == "4" {
				bot.getContact()
			} else {
				msgs, err := bot.sync()
				if err == nil && msgs != nil && len(msgs) > 0 {
					fmt.Println("[INFO] 正在同步消息")
					for _, msg := range msgs {
						SyncWorkQueue <- SyncWorkRequest{Msg: msg}
					}
				}

				if err != nil {
					fmt.Println("[ERROR] 消息同步失败....")
				}
			}
		}

		time.Sleep(time.Second)
	}
	return nil
}

func (bot *WxBot) Run() error {
	if err := bot.getUuid(); err != nil {
		return err
	}

	if err := bot.genQrCode(); err != nil {
		return err
	}

	fmt.Println("[INFO] 请使用手机微信扫该二维码登陆 .")

	if err := bot.wait4login(); err != nil {
		fmt.Println("[ERROR] Web WeChat login failed.")
		return err
	}

	if err := bot.login(); err != nil {
		fmt.Println("[ERROR] Web WeChat login failed.")
		return err
	}
	fmt.Println("[INFO] 登陆成功.")

	if err := bot.init(); err != nil {
		fmt.Println("[ERROR] Web WeChat failed to init")
		return err
	}
	fmt.Println("[INFO] 初始化成功.")

	if err := bot.statusNotify(); err != nil {
		fmt.Println("[ERROR] Web WeChat failed to run status notify")
		return err
	}
	fmt.Println("[INFO] 状态同步成功")

	if err := bot.getContact(); err != nil {
		fmt.Println(err)
		fmt.Println("[ERROR] Web WeChat failed to run get contact")
		retries := 5
		for i := 0; i < retries; i++ {
			if _err := bot.getContact(); _err == nil {
				err = nil
				break
			}
			fmt.Println(err)
		}

		if err != nil {
			return err
		}
	}
	fmt.Println("[INFO] 成功获取通讯录")
	fmt.Println("[INFO] 开始接受处理消息....")

	if err := bot.syncMsg(bot.msgQueueSize, bot.syncWorkers); err != nil {
		fmt.Println("the sync msg error is", err)
		return err
	}
	return nil
}

func (bot *WxBot) formatMsg(msg map[string]interface{}) map[string]interface{} {
	bot.RLock()
	defer bot.RUnlock()

	msgTypeId := 0
	rawMsgType := 0
	if _msgType, ok := msg["MsgType"]; ok {
		rawMsgType = int(_msgType.(float64))
	} else {
		return nil
	}

	fromUserId := msg["FromUserName"].(string)
	fromUserName := "unknown"

	switch rawMsgType {
	case 51:
		msgTypeId = 0
		fromUserName = "system"
	case 37:
		msgTypeId = 37
		return nil
	}

	if msg["FromUserName"] == bot.myAccount["UserName"] {
		msgTypeId = 1
		fromUserName = "self"
	} else if msg["ToUserName"].(string) == "filehelper" {
		msgTypeId = 2
		fromUserName = "filehelper"
	} else if (msg["FromUserName"].(string))[:2] == "@@" {
		msgTypeId = 3
		fromUserName = bot.getContactPreferName(bot.getContactName(fromUserId))
	} else if bot.isContact(msg["FromUserName"].(string)) {
		msgTypeId = 4
		fromUserName = bot.getContactPreferName(bot.getContactName(fromUserId))
	} else if bot.isPublic(msg["FromUserName"].(string)) {
		msgTypeId = 5
		fromUserName = bot.getContactPreferName(bot.getContactName(fromUserId))
	} else if bot.isSpecial(msg["FromUserName"].(string)) {
		msgTypeId = 6
		fromUserName = bot.getContactPreferName(bot.getContactName(fromUserId))
	} else {
		msgTypeId = 99
	}

	fromUserName = html.UnescapeString(fromUserName)

	content := bot.extractMsgContent(msgTypeId, msg)

	fmtMsg := map[string]interface{}{
		"account_id":     bot.myAccount["UserName"],
		"msg_type_id":    msgTypeId,
		"msg_id":         msg["MsgId"].(string),
		"content":        content,
		"to_user_id":     msg["ToUserName"].(string),
		"from_user_name": fromUserName,
		"from_user_id":   fromUserId,
	}

	return fmtMsg
}

func (bot *WxBot) extractMsgContent(msgTypeId int, msg map[string]interface{}) map[string]interface{} {
	msgType := msg["MsgType"].(float64)
	content := html.UnescapeString(msg["Content"].(string))
	msgId := msg["MsgId"].(string)

	msgContent := make(map[string]interface{})
	if msgTypeId == 0 {
		msgContent = map[string]interface{}{"type": 11, "data": ""}
		return msgContent
	} else if msgTypeId == 2 {
		msgContent = map[string]interface{}{"type": 0, "data": strings.Replace(content, "<br/>", "\n", -1)}
		return msgContent
	} else if msgTypeId == 3 {
		sp := strings.Index(content, "<br/>")
		if sp == -1 {
			msgContent["user"] = map[string]interface{}{"id": "", "name": ""}
		} else {
			uid := content[:(sp - 1)]
			content = content[sp:]
			content = strings.Replace(content, "<br/>", "", -1)
			name := bot.getContactPreferName(bot.getContactName(uid))
			if name == "" {
				name = bot.getContactPreferName(bot.getGroupMemberName(msg["FromUserName"].(string), uid))
			}

			if name == "" {
				name = "unknown"
			}

			msgContent["user"] = map[string]interface{}{"id": uid, "name": name}
		}
	}

	switch msgType {
	case 1:
		if strings.Index(content, "http://weixin.qq.com/cgi-bin/redirectforward?args=") != -1 {
		} else {
			msgContent["type"] = 0
			if msgTypeId == 3 || (msgTypeId == 1 && (msg["ToUserName"].(string))[:2] == "@@") {
				msgContent["data"] = content
			} else {
				msgContent["data"] = content
			}
		}
	case 3:
		msgContent["type"] = 3
		msgContent["data"] = fmt.Sprintf("%s/webwxgetmsgimg?MsgID=%s&skey=%s", bot.baseUri, msgId, bot.skey)
	case 34:
		msgContent["type"] = 4
		msgContent["data"] = fmt.Sprintf("%s/webwxgetvoice?MsgID=%s&skey=%s", bot.baseUri, msgId, bot.skey)
	case 49:
		msgContent["type"] = 7
		appMsgType := msg["AppMsgType"].(float64)
		_appMsgType := ""
		switch appMsgType {
		case 3:
			_appMsgType = "music"
		case 5:
			_appMsgType = "link"
		case 7:
			_appMsgType = "weibo"
		default:
			_appMsgType = "unknown"
		}

		msgContent["data"] = map[string]interface{}{
			"type":    _appMsgType,
			"title":   msg["FileName"],
			"url":     msg["Url"],
			"content": msg["Content"],
		}

	case 10000:
		if strings.Index(content, "加入了群聊") != -1 || strings.Index(content, "分享的二维码加入群聊") != -1 {
			msgContent["type"] = 12
		}

		if strings.Index(content, "移出了群聊") != -1 {
			msgContent["type"] = 13
		}
		msgContent["data"] = content
	default:
		msgContent["type"] = 99
		msgContent["data"] = content
	}
	return msgContent
}

func (bot *WxBot) isBadResponse(res map[string]interface{}) error {
	if _badRes, ok := res["BaseResponse"]; ok {
		badRes := _badRes.(map[string]interface{})
		if ret, ok := badRes["Ret"]; ok {
			if ret.(float64) != 0 {
				return errors.New("bad response")
			}
		}
	}
	return nil
}

func (bot *WxBot) isContact(uid string) bool {
	for _, contact := range bot.contactList {
		if contact.UserName == uid {
			return true
		}
	}

	return false
}

func (bot *WxBot) isPublic(uid string) bool {
	for _, contact := range bot.publicList {
		if contact.UserName == uid {
			return true
		}
	}

	return false
}

func (bot *WxBot) isSpecial(uid string) bool {
	for _, contact := range bot.specialList {
		if contact.UserName == uid {
			return true
		}
	}

	return false
}

func (bot *WxBot) getContactName(uid string) map[string]string {
	member, ok := bot.accountInfo["normal_member"][uid]
	if !ok {
		return nil
	}
	info := member.Info
	name := make(map[string]string)
	if info.RemarkName != "" {
		name["remark_name"] = info.RemarkName
	}
	if info.NickName != "" {
		name["nickname"] = info.NickName
	}
	if info.DisplayName != "" {
		name["'display_name"] = info.DisplayName
	}
	if len(name) != 0 {
		return name
	}
	return nil
}

func (bot *WxBot) getContactPreferName(name map[string]string) string {
	if name == nil {
		return ""
	}
	if _name, ok := name["remark_name"]; ok {
		return _name
	}
	if _name, ok := name["nickname"]; ok {
		return _name
	}
	if _name, ok := name["display_name"]; ok {
		return _name
	}
	return ""
}

func (bot *WxBot) getGroupMemberName(gid, uid string) map[string]string {
	members, ok := bot.groupMembers[gid]
	if !ok {
		return nil
	}
	for _, member := range members {
		if member.UserName == uid {
			name := make(map[string]string)
			if member.RemarkName != "" {
				name["remark_name"] = member.RemarkName
			}
			if member.NickName != "" {
				name["nickname"] = member.NickName
			}
			if member.DisplayName != "" {
				name["display_name"] = member.DisplayName
			}
			return name
		}
	}
	return nil
}

func (bot *WxBot) SendMsgById(content, toId string) error {
	url := fmt.Sprintf("%s/webwxsendmsg?pass_ticket=%s", bot.baseUri, bot.passTicket)
	msgId := MsgId()

	data := map[string]interface{}{
		"BaseRequest": bot.baseRequest,
		"Msg": map[string]interface{}{
			"Type":         1,
			"Content":      content,
			"FromUserName": bot.myAccount["UserName"],
			"ToUserName":   toId,
			"LocalID":      msgId,
			"ClientMsgId":  msgId,
		},
	}

	res, _, err := MakeHttpRequest(POST, url, data, bot.cookie)
	if err != nil {
		fmt.Printf("Failed to send the message with err: %+v\n", err)
		return err
	}

	var resMap map[string]interface{}
	if err := json.Unmarshal([]byte(res), &resMap); err != nil {
		return err
	}

	if err := bot.isBadResponse(resMap); err != nil {
		return err
	}

	return nil
}

func (bot *WxBot) SendImgMsgById(path, toId string) error {
	//fmt.Println("[DEBUG] Entering send img msg by id")
	mediaId, err := bot.UploadMedia(path, true)
	if err != nil {
		return err
	}
	//fmt.Println("[DEBUG] media id is", mediaId)

	url := fmt.Sprintf("%s/webwxsendmsgimg?fun=async&f=json", bot.baseUri)
	msgId := MsgId()
	data := map[string]interface{}{
		"BaseRequest": bot.baseRequest,
		"Msg": map[string]interface{}{
			"Type":         3,
			"MediaId":      mediaId,
			"FromUserName": bot.myAccount["UserName"],
			"ToUserName":   toId,
			"LocalID":      msgId,
			"ClientMsgId":  msgId,
		},
	}

	res, _, err := MakeHttpRequest(POST, url, data, bot.cookie)
	if err != nil {
		return err
	}

	var resMap map[string]interface{}
	if err := json.Unmarshal([]byte(res), &resMap); err != nil {
		return err
	}

	if err := bot.isBadResponse(resMap); err != nil {
		return err
	}

	return nil
}

func (bot *WxBot) UploadMedia(path string, isImg bool) (string, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		fmt.Printf("[ERROR] the file %s to upload does not exist", path)
		return "", err
	}

	parseUrl, _ := url.Parse(bot.baseUri)

	wxDataTicket := ""
	for _, _cookie := range bot.cookie.Cookies(parseUrl) {
		if _cookie.Name == "webwx_data_ticket" {
			wxDataTicket = _cookie.Value
			break
		}
	}

	if wxDataTicket == "" {
		return "", errors.New("No data ticket is found")
	}

	fileSize := strconv.FormatInt(fileInfo.Size(), 10)

	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	buffer := make([]byte, 512)
	_, err = file.Read(buffer)
	if err != nil {
		return "", err
	}

	mediaType := "pic"
	if !isImg {
		mediaType = "doc"
	}

	contentType := http.DetectContentType(buffer)

	uploadMediaRequest := map[string]interface{}{
		"BaseRequest":   bot.baseRequest,
		"ClientMediaId": time.Now().Unix(),
		"TotalLen":      fileSize,
		"StartPos":      0,
		"DataLen":       fileSize,
		"MediaType":     4,
	}

	uploadMediaRequestStr, _ := json.Marshal(uploadMediaRequest)

	files := map[string]string{
		"id":                 "WU_FILE_" + strconv.FormatInt(time.Now().Unix()*1000, 10),
		"name":               fileInfo.Name(),
		"type":               contentType,
		"lastModifiedDate":   "Thu Mar 17 2016 00:55:10 GMT+0800 (CST)",
		"size":               fileSize,
		"mediatype":          mediaType,
		"uploadmediarequest": string(uploadMediaRequestStr),
		"pass_ticket":        bot.passTicket,
		"webwx_data_ticket":  wxDataTicket,
	}

	url := "https://file.wx.qq.com/cgi-bin/mmwebwx-bin/webwxuploadmedia?f=json"
	request, err := NewFileUploadRequest(url, files, "filename", path)

	client := &http.Client{Jar: bot.cookie}
	resp, err := client.Do(request)
	defer resp.Body.Close()

	resBody, err := ioutil.ReadAll(resp.Body)

	fmt.Printf("[DEBUG] upload media response is %s \n", string(resBody))

	var resMap map[string]interface{}
	if err := json.Unmarshal(resBody, &resMap); err != nil {
		return "", err
	}

	if err := bot.isBadResponse(resMap); err != nil {
		return "", err
	}

	return resMap["MediaId"].(string), nil
}

func (bot *WxBot) AccountId() string {
	return bot.myAccount["UserName"].(string)
}
