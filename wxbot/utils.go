package wxbot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	//u "net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

const (
	POST  = "POST"
	GET   = "GET"
	PUT   = "PUT"
	PATCH = "PATCH"

	BLACK = "\033[40m  \033[0m"
	WHITE = "\033[47m  \033[0m"
)

func MakeHttpRequest(method, url string, entity map[string]interface{}, jar *cookiejar.Jar) (string, int, error) {
	var body io.Reader
	var err error

	if entity != nil {
		switch method {
		case POST, PUT, PATCH:
			b, err := json.Marshal(entity)
			if err != nil {
				return "", 0, err
			}

			b = bytes.Replace(b, []byte("\\u003c"), []byte("<"), -1)
			b = bytes.Replace(b, []byte("\\u003e"), []byte(">"), -1)
			b = bytes.Replace(b, []byte("\\u0026"), []byte("&"), -1)

			body = bytes.NewBuffer(b)

		case GET:
			if len(entity) > 0 {
				params := make([]string, len(entity))
				index := 0
				for k, v := range entity {
					_v := fmt.Sprintf("%v", v)
					//params[index] = fmt.Sprintf("%s=%v", k, u.QueryEscape(_v))
					params[index] = fmt.Sprintf("%s=%v", k, _v)
					index++
				}
				queryStr := strings.Join(params, "&")
				//queryStr = u.QueryEscape(queryStr)
				url = fmt.Sprintf("%s?%s", url, queryStr)
			}
		}
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return "", 0, err
	}

	if entity != nil && (method == POST || method == PUT || method == PATCH) {
		req.Header.Set("Content-Type", "application/json;charset=utf-8")
		req.Header.Set("Accept", "application/json")
	}
	req.Header.Add("Connection", "close")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux i686; U;) Gecko/20070322 Kazehakase/0.4.5")

	client := http.DefaultClient
	if jar != nil {
		client = &http.Client{
			Jar: jar,
		}
	}
	//fmt.Println(req.Header)
	//fmt.Println(jar)

	res, err := client.Do(req)
	if err != nil {
		fmt.Println("faild to do the request with error ", err)
		return "", 0, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusNoContent {
		fmt.Println("code is not 200 ", res.StatusCode)
		return "", 0, errors.New("http request failed to call")
	}
	resBody, err := ioutil.ReadAll(res.Body)
	if err != nil {
		fmt.Println("could not read the response body")
		return "", 0, errors.New("the response could not be read")
	}

	return string(resBody), res.StatusCode, nil
}

func generateQrcode2Terminal(source string) (string, error) {
	q, err := qrcode.New(source, qrcode.Medium)
	if err != nil {
		return "", err
	}

	var fmtBitmap [][]bool
	borderWidth := 2

	bitmap := q.Bitmap()
	for i := borderWidth; i < len(bitmap)-borderWidth; i++ {
		row := bitmap[i]
		fmtBitmap = append(fmtBitmap, row[borderWidth:len(row)-borderWidth])
	}

	out := ""
	for _, row := range fmtBitmap {
		for _, cell := range row {
			if cell {
				out += BLACK
			} else {
				out += WHITE
			}
		}
		out += "\n"
	}

	return out, nil
}

func Contains(obj interface{}, target interface{}) bool {
	targetValue := reflect.ValueOf(target)
	switch reflect.TypeOf(target).Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < targetValue.Len(); i++ {
			if targetValue.Index(i).Interface() == obj {
				return true
			}
		}
	case reflect.Map:
		if targetValue.MapIndex(reflect.ValueOf(obj)).IsValid() {
			return true
		}
	}
	return false
}

func MsgId() string {
	return fmt.Sprintf("%s%s", strconv.FormatInt(time.Now().Unix()*1000, 10), strings.Replace(strconv.FormatFloat(rand.Float64(), 'f', -1, 64)[:5], ".", "", -1))
}

func NewFileUploadRequest(uri string, params map[string]string, paramName, path string) (*http.Request, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.SetBoundary("WebKitFormBoundaryHl8UnshLMOcMf1B1")

	for key, val := range params {
		_ = writer.WriteField(key, val)
	}

	part, err := writer.CreateFormFile(paramName, filepath.Base(path))
	if err != nil {
		return nil, err
	}
	_, err = io.Copy(part, file)

	err = writer.Close()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", uri, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux i686; U;) Gecko/20070322 Kazehakase/0.4.5")
	return req, err
}
