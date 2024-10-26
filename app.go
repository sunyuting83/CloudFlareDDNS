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
}

type FormConfig struct {
	ZoneID     string `form:"ZoneID" json:"ZoneID" xml:"ZoneID"  binding:"required"`
	Token      string `form:"Token" json:"Token" xml:"Token"  binding:"required"`
	Type       string `form:"Type" json:"Type" xml:"Type"  binding:"required"`
	Proxy      string `form:"Proxy" json:"Proxy" xml:"Proxy"  binding:"required"`
	Interfaces string `form:"Interfaces" json:"Interfaces" xml:"Interfaces"  binding:"required"`
	Domains    string `form:"Domains" json:"Domains" xml:"Domains"  binding:"required"`
}

type FormPassword struct {
	Password string `form:"Password" json:"Password" xml:"Password"  binding:"required"`
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
	var config *Config
	if config.Types == 0 {
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
			}

			// 将默认配置写入新文件
			file, err := os.Create(ConfigFile)
			if err != nil {
				return confYaml, errors.New("Error creating config file\nThe program will close in 10 seconds")
			}
			defer file.Close()

			config, _ := yaml.Marshal(&confYaml)
			os.WriteFile(ConfigFile, config, 0644)
		}
	} else {
		err = yaml.Unmarshal(yamlFile, &confYaml)
		if err != nil {
			return confYaml, errors.New("Error read config file\nThe program will close in 10 seconds")
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
		return confYaml, nil
	}
	return confYaml, nil
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
		confYaml.Domains = Domains
		confYaml.ZoneID = form.ZoneID
		confYaml.Token = form.Token
		confYaml.Types = Types
		confYaml.Proxy = Proxy
		confYaml.InterFace = form.Interfaces
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
