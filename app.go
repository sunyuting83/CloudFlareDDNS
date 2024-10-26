package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v2"
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
	CFApi     string
	Proxy     bool
	ZoneID    string
	Token     string
	InterFace string
	Types     int
	IPAddr    string
	IP6Addr   string
	AdminPWD  string
	Domains   string
	ScanTime  int
	HasError  bool
}

type FormConfig struct {
	ZoneID     string `form:"ZoneID" json:"ZoneID" xml:"ZoneID"  binding:"required"`
	Token      string `form:"Token" json:"Token" xml:"Token"  binding:"required"`
	Type       string `form:"Type" json:"Type" xml:"Type"  binding:"required"`
	Proxy      string `form:"Proxy" json:"Proxy" xml:"Proxy"  binding:"required"`
	Interfaces string `form:"Interfaces" json:"Interfaces" xml:"Interfaces"  binding:"required"`
	Domains    string `form:"Domains" json:"Domains" xml:"Domains"  binding:"required"`
	ScanTime   string `form:"ScanTime" json:"ScanTime" xml:"ScanTime"  binding:"required"`
}

type FormPassword struct {
	Password string `form:"Password" json:"Password" xml:"Password"  binding:"required"`
}

type IPList struct {
	IPAddr  string
	IP6Addr string
}

var CfStatus string = "nothing"
var CacheUrl string

func FilterString(input string) string {
	// 使用 ReplaceAll 方法过滤掉 \r、\n 和 \t
	input = strings.ReplaceAll(input, "\r", "")
	input = strings.ReplaceAll(input, "\n", "")
	input = strings.ReplaceAll(input, "\t", "")
	return input
}

// GetIpAddr get ip addr
func GetIpAddr(InterFace string) (ip_list *IPList) {
	var (
		ipv4 string
		ipv6 string
	)
	command := strings.Join([]string{"ip -4 addr show dev", InterFace, `| grep "scope global" | awk '{print $2}' | awk -F "/" '{print $1}'`}, " ")
	ip, err := RunCommandWithRes(command)
	if err != nil || len(ip) == 0 {
		ipData, err := getData("4.ipw.cn", "GET", []byte(""), "")
		if err == nil {
			ipv4 = string(ipData)
		}
	} else {
		ipv4 = FilterString(ip)
	}

	v6command := strings.Join([]string{"ip -6 addr show dev", InterFace, `| grep "scope global" | awk '{print $2}' | awk -F "/" '{print $1}'`}, " ")
	v6ip, err := RunCommandWithRes(v6command)
	if err != nil || len(v6ip) == 0 {
		ip6Data, err := getData("https://6.ipw.cn/", "GET", []byte(""), "")
		if err == nil {
			ipv6 = string(ip6Data)
		}
	} else {
		if strings.Contains(v6ip, "\n") {
			ipv6 = strings.Split(v6ip, "\n")[0]
		} else {
			ipv6 = v6ip
		}
	}
	ip_list = &IPList{IPAddr: ipv4, IP6Addr: ipv6}
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
	// fmt.Println(string(d))
	if err != nil {
		return []byte(""), err
	}
	return d, nil
}

// CloudFlareApi Cloud Flare Api
func CloudFlareApi(url string, types string, password string, data []byte, getIP bool) (recordIp string, recordId string, resSuccess bool) {
	d, err := getData(url, types, data, password)
	if err != nil {
		// fmt.Println(err)
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
	d := &PostData{
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

func GetCurrentPath() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(path)
	return dir, nil
}

func CheckConfig(ConfigFile string) (conf *Config, err error) {

	var confYaml *Config
	yamlFile, err := os.ReadFile(ConfigFile)
	if err != nil {
		if os.IsNotExist(err) {
			confYaml = &Config{
				CFApi:     "https://api.cloudflare.com/client/v4/zones/",
				Proxy:     false,
				ZoneID:    "",
				Token:     "",
				InterFace: "",
				IPAddr:    "",
				IP6Addr:   "",
				AdminPWD:  "1234567890",
				ScanTime:  30,
				HasError:  false,
			}

			// 将默认配置写入新文件
			file, err := os.Create(ConfigFile)
			if err != nil {
				return confYaml, errors.New("error creating config file\nthe program will close in 10 seconds")
			}
			defer file.Close()

			config, _ := yaml.Marshal(&confYaml)
			os.WriteFile(ConfigFile, config, 0644)
		}
	} else {
		err = yaml.Unmarshal(yamlFile, &confYaml)
		if err != nil {
			return confYaml, errors.New("error read config file\nthe program will close in 10 seconds")
		}
		if len(confYaml.CFApi) <= 0 {
			confYaml.CFApi = "https://api.cloudflare.com/client/v4/zones/"
			config, _ := yaml.Marshal(&confYaml)
			os.WriteFile(ConfigFile, config, 0644)
		}
		if len(confYaml.AdminPWD) <= 0 {
			confYaml.AdminPWD = "1234567890"
			config, _ := yaml.Marshal(&confYaml)
			os.WriteFile(ConfigFile, config, 0644)
		}
		if confYaml.ScanTime <= 0 {
			confYaml.ScanTime = 30
			config, _ := yaml.Marshal(&confYaml)
			os.WriteFile(ConfigFile, config, 0644)
		}
		return confYaml, nil
	}
	return confYaml, nil
}

func CloudFlareFunc(ipData *IPList, CfRootUrl, CheckUrl, ConfigFile string, config *Config, RecordType string) {
	// fmt.Println(CheckUrl)
	recordId, recordIp, resSuccess := CloudFlareApi(CheckUrl, "GET", config.Token, []byte(""), true)
	// fmt.Println(config.IP6Addr, config.IPAddr, config.Domains, recordIp, resSuccess, config.HasError)
	if resSuccess && len(recordId) != 0 {
		// fmt.Println(recordId, recordIp, resSuccess)
		// fmt.Println(recordIp, ipAddr)
		var DataIP string = ipData.IPAddr

		if RecordType == "A" {
			if config.IPAddr != "" {
				if recordIp == ipData.IPAddr {
					CfStatus = "IPv4 No need to update"
					if CacheUrl == config.Domains {
						return
					}
				}
			}
		}
		if RecordType == "AAAA" {
			DataIP = ipData.IP6Addr
			if config.IP6Addr != "" {
				if recordIp == ipData.IP6Addr {
					CfStatus = "IPv6 No need to update"
					if CacheUrl == config.Domains {
						return
					}
				}
			}
		}
		data := MakePostData(config.Proxy, DataIP, config.Domains, RecordType)
		if recordId == "null" {
			_, _, success := CloudFlareApi(CfRootUrl, "POST", config.Token, data, true)
			// fmt.Println(recordIp)
			if success {
				if RecordType == "A" {
					config.IPAddr = ipData.IPAddr
				} else {
					config.IP6Addr = ipData.IP6Addr
				}

				configData, _ := yaml.Marshal(&config)
				err := os.WriteFile(ConfigFile, configData, 0644)
				if err != nil {
					CfStatus = "Save config file failed for Create"
					return
				}
				CfStatus = "Created successfully"
				CacheUrl = config.Domains
				return
			} else {
				CfStatus = "Authentication failed for Create"
				return
			}
		} else {
			var updateDnsApi string = strings.Join([]string{CfRootUrl, "/", recordId}, "")
			// fmt.Println(updateDnsApi)
			_, _, success := CloudFlareApi(updateDnsApi, "PUT", config.Token, data, false)
			if success {
				if RecordType == "A" {
					config.IPAddr = ipData.IPAddr
				} else {
					config.IP6Addr = ipData.IP6Addr
				}
				configData, _ := yaml.Marshal(&config)
				err := os.WriteFile(ConfigFile, configData, 0644)
				// fmt.Println(recordIp, success, "up", config)
				if err != nil {
					CfStatus = "Save config file failed for Update"
					return
				}
				CfStatus = "Update completed"
				CacheUrl = config.Domains
				return
			} else {
				CfStatus = "Authentication failed for Update"
				return
			}
		}
	} else {
		config.HasError = true

		configData, _ := yaml.Marshal(&config)
		err := os.WriteFile(ConfigFile, configData, 0644)
		if err != nil {
			CfStatus = "Save config file failed for HasError"
			return
		}
	}
}

func CronTask(ScanTime int, confYaml *Config, ConfigFile string) (chan bool, chan string) {

	ticker := time.NewTicker(time.Duration(ScanTime) * time.Second)

	stopChan := make(chan bool)
	statusChan := make(chan string) // 新增状态通道

	task := func() {
		if CacheUrl != confYaml.Domains {
			confYaml.IPAddr = ""
			confYaml.IP6Addr = ""
		}
		var CfRootUrl string = strings.Join([]string{confYaml.CFApi, confYaml.ZoneID, "/dns_records"}, "")
		var CheckUrl string = strings.Join([]string{CfRootUrl, "?name=", confYaml.Domains, "&type="}, "")
		ipData := GetIpAddr(confYaml.InterFace)
		// fmt.Println(ipData)
		switch confYaml.Types {
		case 0:
			if confYaml.IPAddr != ipData.IPAddr {
				v4url := strings.Join([]string{CheckUrl, "A"}, "")
				CloudFlareFunc(ipData, CfRootUrl, v4url, ConfigFile, confYaml, "A")
			} else {
				CfStatus = "IPv4 No need to update"
			}

		case 1:
			if confYaml.IP6Addr != ipData.IP6Addr {
				v6url := strings.Join([]string{CheckUrl, "AAAA"}, "")
				CloudFlareFunc(ipData, CfRootUrl, v6url, ConfigFile, confYaml, "AAAA")
			} else {
				CfStatus = "IPv6 No need to update"
			}
		case 2:
			if confYaml.IPAddr != ipData.IPAddr {
				v4url := strings.Join([]string{CheckUrl, "A"}, "")
				CloudFlareFunc(ipData, CfRootUrl, v4url, ConfigFile, confYaml, "A")
			}
			if confYaml.IP6Addr != ipData.IP6Addr {
				v6url := strings.Join([]string{CheckUrl, "AAAA"}, "")
				CloudFlareFunc(ipData, CfRootUrl, v6url, ConfigFile, confYaml, "AAAA")
			} else {
				CfStatus = "IPv4 And IPv6 No need to update"
			}

		}
	}

	go func(ticker *time.Ticker) {
		defer ticker.Stop()
		statusChan <- "running" // 任务开始时发送状态

		for {
			select {
			case <-ticker.C:
				if strings.Contains(confYaml.Domains, ".") && len(confYaml.Token) >= 30 && len(confYaml.ZoneID) >= 30 && !confYaml.HasError {
					task()
				}
			case stop := <-stopChan:
				if stop {
					statusChan <- "stopped" // 任务停止时发送状态
					return
				}
			}
		}
	}(ticker)

	return stopChan, statusChan // 返回状态通道
}

func FilterURL(url string) string {
	// 替换 http:// 和 https://
	url = strings.ReplaceAll(url, "http://", "")
	url = strings.ReplaceAll(url, "https://", "")

	// 移除所有的 /
	url = strings.ReplaceAll(url, "/", "")

	return url
}

func main() {
	CurrentPath, _ := GetCurrentPath()
	ConfigFile := strings.Join([]string{CurrentPath, "config.yaml"}, "/")
	confYaml, err := CheckConfig(ConfigFile)
	if err != nil {
		fmt.Println(err)
		time.Sleep(10 * time.Second)
		os.Exit(1)
	}
	CacheUrl = confYaml.Domains
	// fmt.Println(CacheUrl)
	var status string
	ch, statusChan := CronTask(1, confYaml, ConfigFile)
	go func() {
		for {
			select {
			case status = <-statusChan:
				fmt.Println("CronTask status:", status)
			default:
				time.Sleep(3000 * time.Millisecond) // 暂停 100 毫秒
			}
		}
	}()

	router := gin.Default()

	accounts := gin.Accounts{
		"admin": confYaml.AdminPWD,
	}
	router.Use(gin.BasicAuth(accounts))

	router.GET("/", func(c *gin.Context) {
		user := c.MustGet(gin.AuthUserKey).(string)
		password := accounts["admin"]
		c.JSON(http.StatusOK, gin.H{
			"message": "Welcome to the admin dashboard!",
			"user":    user,
			"passwod": password,
		})
	})

	router.GET("/api/status", func(c *gin.Context) {
		var interfacesList []string
		interfaces, err := net.Interfaces()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"status":  500,
				"message": err.Error(),
			})
			return
		}

		for _, iface := range interfaces {
			if iface.Flags&net.FlagUp != 0 {
				interfacesList = append(interfacesList, iface.Name)
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"status":         200,
			"message":        "success",
			"ZoneID":         confYaml.ZoneID,
			"Token":          confYaml.Token,
			"IPv4":           confYaml.IPAddr,
			"IPv6":           confYaml.IP6Addr,
			"Type":           confYaml.Types,
			"Proxy":          confYaml.Proxy,
			"InterFacesList": interfacesList,
			"Domains":        confYaml.Domains,
			"InterFace":      confYaml.InterFace,
			"TaskStatus":     status,
			"ApiStatus":      CfStatus,
			"HasError":       confYaml.HasError,
			"ScanTime":       confYaml.ScanTime,
		})
	})

	router.PUT("/api/setconfig", func(c *gin.Context) {
		var form FormConfig
		if err := c.ShouldBind(&form); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"status":  1,
				"message": err.Error(),
			})
			return
		}
		if len(form.ZoneID) < 30 {
			c.JSON(http.StatusBadRequest, gin.H{
				"status":  1,
				"message": "Invalid ZoneID",
			})
			return
		}
		if len(form.Token) < 30 {
			c.JSON(http.StatusBadRequest, gin.H{
				"status":  1,
				"message": "Invalid Token",
			})
			return
		}
		if !strings.Contains(form.Domains, ".") {
			c.JSON(http.StatusBadRequest, gin.H{
				"status":  1,
				"message": "Invalid Domains",
			})
			return
		}
		Domains := FilterURL(form.Domains)

		var Types int
		switch form.Type {
		case "0":
			Types = 0
		case "1":
			Types = 1
		case "2":
			Types = 2
		default:
			Types = 0
		}
		var Proxy bool
		switch form.Proxy {
		case "true":
			Proxy = true
		case "false":
			Proxy = false
		default:
			Proxy = false
		}
		ScanTime, err := strconv.Atoi(form.ScanTime)
		if err != nil {
			ScanTime = 30
		}
		confYaml.Domains = Domains
		confYaml.ZoneID = form.ZoneID
		confYaml.Token = form.Token
		confYaml.Types = Types
		confYaml.Proxy = Proxy
		confYaml.InterFace = form.Interfaces
		confYaml.ScanTime = ScanTime
		if confYaml.HasError {
			confYaml.HasError = false
		}
		// fmt.Println(confYaml.HasError)
		config, _ := yaml.Marshal(&confYaml)
		err = os.WriteFile(ConfigFile, config, 0644)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status":  500,
				"message": "Error writing config file",
			})
			return
		}
		// fmt.Println(status)
		if status == "stopped" {
			ch <- false
		}

		c.JSON(http.StatusOK, gin.H{
			"status":  200,
			"message": "Save config file",
		})
	})

	router.PUT("/api/setpassword", func(c *gin.Context) {
		var form FormPassword
		if err := c.ShouldBind(&form); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"status":  1,
				"message": err.Error(),
			})
			return
		}
		if len(form.Password) < 8 {
			c.JSON(http.StatusBadRequest, gin.H{
				"status":  1,
				"message": "Password must be at least 8 characters",
			})
			return
		}
		confYaml.AdminPWD = form.Password
		config, _ := yaml.Marshal(&confYaml)
		err := os.WriteFile(ConfigFile, config, 0644)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status":  500,
				"message": "Error writing config file",
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status":  200,
			"message": "Changed Password",
		})
	})

	srv := &http.Server{
		Addr:    ":3060",
		Handler: router,
	}
	fmt.Printf("listen port %s\n", srv.Addr)
	go func() {
		// 服务连接
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit
	log.Println("Shutdown Server ...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server Shutdown:", err)
	}
	log.Println("Server exiting")
}
