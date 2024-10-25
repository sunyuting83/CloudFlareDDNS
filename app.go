package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Success struct {
	Result     []Result      `json:"result"`
	Success    bool          `json:"success"`
	Errors     []interface{} `json:"errors"`
	Messages   []interface{} `json:"messages"`
	ResultInfo ResultInfo    `json:"result_info"`
}
type Meta struct {
	AutoAdded           bool   `json:"auto_added"`
	ManagedByApps       bool   `json:"managed_by_apps"`
	ManagedByArgoTunnel bool   `json:"managed_by_argo_tunnel"`
	Source              string `json:"source"`
}
type Result struct {
	ID         string    `json:"id"`
	ZoneID     string    `json:"zone_id"`
	ZoneName   string    `json:"zone_name"`
	Name       string    `json:"name"`
	Type       string    `json:"type"`
	Content    string    `json:"content"`
	Proxiable  bool      `json:"proxiable"`
	Proxied    bool      `json:"proxied"`
	TTL        int       `json:"ttl"`
	Locked     bool      `json:"locked"`
	Meta       Meta      `json:"meta"`
	CreatedOn  time.Time `json:"created_on"`
	ModifiedOn time.Time `json:"modified_on"`
}
type ResultInfo struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	Count      int `json:"count"`
	TotalCount int `json:"total_count"`
	TotalPages int `json:"total_pages"`
}

type PostData struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
}

type Config struct {
	CFApi      string
	Proxy      bool
	ZoneID     string
	Token      string
	HostName   string
	InterFace  string
	Types      bool
	IPAddr     string
	RecordType string
}

var config *Config

func main() {
	var (
		proxy      bool   = false
		zoneid     string = os.Args[1]
		token      string = os.Args[2]
		hostname   string = os.Args[3]
		interFace  string = os.Args[4]
		ifv4       bool   = false
		recordType string = "A"
	)
	if len(os.Args) >= 7 {
		if len(os.Args[1]) != 32 {
			fmt.Println("zoneid is error")
			return
		}
		if len(os.Args[2]) != 40 {
			fmt.Println("token is error")
			return
		}
		if len(os.Args[3]) <= 5 {
			fmt.Println("hostname is error")
			return
		}
		if len(os.Args[4]) <= 1 {
			fmt.Println("interface is error")
			return
		}
		if os.Args[5] == "4" {
			ifv4 = true
		} else {
			ifv4 = false
			recordType = "AAAA"
		}
		if os.Args[6] == "true" {
			proxy = true
		}
	} else {
		fmt.Println("params is not supported")
		return
	}
	config = &Config{
		CFApi:      "https://api.cloudflare.com/client/v4/zones/",
		ZoneID:     zoneid,
		Token:      token,
		HostName:   hostname,
		Types:      ifv4,
		InterFace:  interFace,
		RecordType: recordType,
	}
	ipAddr := GetIpAddr()
	ip, a := ParseIP(ipAddr)
	if ip != nil {
		if a != 4 {
			recordType = "AAAA"
		}
	}
	var CurrentUrl string = strings.Join([]string{config.CFApi, config.ZoneID, "/dns_records?type=", config.RecordType, "&name=", config.HostName}, "")
	recordId, recordIp, resSuccess := CloudFlareApi(CurrentUrl, "GET", config.Token, []byte(""), true)
	if resSuccess {
		// fmt.Println(recordIp, ipAddr)
		if recordIp == ipAddr {
			fmt.Println("nochg")
			return
		}
		data := MakePostData(proxy, ipAddr, config.HostName, config.RecordType)
		if recordId == "null" {
			var createDnsApi string = strings.Join([]string{config.CFApi, config.ZoneID, "/dns_records"}, "")
			_, _, success := CloudFlareApi(createDnsApi, "POST", config.Token, data, false)
			if success {
				fmt.Println("good")
				return
			} else {
				fmt.Println("badauth")
				return
			}
		} else {
			var updateDnsApi string = strings.Join([]string{config.CFApi, config.ZoneID, "/dns_records/", recordId}, "")
			_, _, success := CloudFlareApi(updateDnsApi, "PUT", config.Token, data, false)
			if success {
				fmt.Println("good")
				return
			} else {
				fmt.Println("badauth")
				return
			}
		}
	}
	fmt.Println("badauth")
	return
}

// ParseIP Parse IP Type
func ParseIP(s string) (net.IP, int) {
	ip := net.ParseIP(s)
	if ip == nil {
		return nil, 0
	}
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '.':
			return ip, 4
		case ':':
			return ip, 6
		}
	}
	return nil, 0
}

// GetIpAddr get ip addr
func GetIpAddr() (i string) {
	if config.Types {
		command := strings.Join([]string{"ip -4 addr show dev", config.InterFace, `| grep "scope global" | awk '{print $2}' | awk -F "/" '{print $1}'`}, " ")
		ip, err := RunCommandWithRes(command)
		if err != nil || len(ip) == 0 {
			ip, err := getData("4.ipw.cn", "GET", []byte(""), "")
			if err == nil {
				i = string(ip)
			}
		}
		if strings.Contains(i, "\n") {
			i = strings.Split(i, "\n")[0]
			return
		}
		i = ip
		return
	}
	command := strings.Join([]string{"ip -6 addr show dev", config.InterFace, `| grep "scope global" | awk '{print $2}' | awk -F "/" '{print $1}'`}, " ")
	i, err := RunCommandWithRes(command)
	if err != nil || len(i) == 0 {
		ip, err := getData("https://6.ipw.cn/", "GET", []byte(""), "")
		if err == nil {
			i = string(ip)
		}
	}
	if strings.Contains(i, "\n") {
		i = strings.Split(i, "\n")[0]
	}
	return
}

// getData get data
func getData(url string, types string, data []byte, password string) (s []byte, err error) {
	client := &http.Client{
		Timeout: time.Duration(15 * time.Second),
	}
	reqest, err := http.NewRequest(types, url, bytes.NewBuffer(data))

	if len(password) > 0 {
		reqest.Header.Add("Authorization", strings.Join([]string{"Bearer", password}, " "))
	}

	if err != nil {
		return []byte(""), err
	}
	response, err := client.Do(reqest)
	if err != nil {
		return []byte(""), err
	}
	defer response.Body.Close()
	d, err := io.ReadAll(response.Body)
	if err != nil {
		return []byte(""), err
	}
	return d, nil
}

// CloudFlareApi Cloud Flare Api
func CloudFlareApi(url string, types string, password string, data []byte, getIP bool) (recordIp string, recordId string, resSuccess bool) {
	d, err := getData(url, types, data, password)
	if err != nil {
		return "", "", false
	}
	var p *Success
	//3.json解析到结构体
	if err := json.Unmarshal(d, &p); err != nil {
		return "", "", p.Success
	}
	if p.Success {
		if getIP {
			if len(p.Result) > 0 {
				return p.Result[0].ID, p.Result[0].Content, p.Success
			}
			return "", "", p.Success
		}
		return "", "", p.Success
	}
	return "", "", p.Success
}

// MakePostData Make post data
func MakePostData(proxy bool, ipAddr string, hostname string, recordType string) (bd []byte) {
	var d *PostData
	d = &PostData{
		Type:    recordType,
		Name:    hostname,
		Content: ipAddr,
		Proxied: proxy,
	}
	b, _ := json.Marshal(d)
	return b
}

func RunCommandWithRes(cmdExec string) (k string, err error) {
	cmd := exec.Command("/bin/sh", "-c", cmdExec)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	defer stdout.Close()

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	defer stderr.Close()

	if err := cmd.Start(); err != nil {
		return "", err
	}

	bytesErr, err := io.ReadAll(stderr)
	if err != nil {
		return "", err
	}

	if len(bytesErr) != 0 {
		return "", errors.New("0")

	}

	bytes, err := io.ReadAll(stdout)
	if err != nil {
		return "", err
	}

	if err := cmd.Wait(); err != nil {
		return "", err
	}
	return string(bytes), nil
}
