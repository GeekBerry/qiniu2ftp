// ftpServer project main.go
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"qiniupkg.com/api.v7/kodo"
)

var conf map[string]string

func init() {
	log.SetFlags(log.Ltime | log.Lshortfile)
}

func main() {

	//载入配置文件
	b, err := ioutil.ReadFile("conf.json")
	if err != nil {
		log.Panic(err)
	}
	err = json.Unmarshal(b, &conf)
	if err != nil {
		log.Panic(err)
	}

	//设置七牛云key
	kodo.SetMac(conf["ACCESS_KEY"], conf["SECRET_KEY"])

	//监听启用服务
	s, err := net.Listen("tcp", ":2121")
	if err != nil {
		log.Panic(err)
	}
	log.Println("ftp服务运行中:ftp://127.0.0.1:2121")
	for {
		c, err := s.Accept()
		log.Println("一个新连接")
		if err != nil {
			log.Panic(err)
		}
		go ftpServer(c)
	}
}

func ftpServer(c net.Conn) {
	defer c.Close()

	var k = kodo.New(0, nil)
	p := k.Bucket(conf["Bucket"])

	r := bufio.NewReader(c)
	//主动打招呼
	fmt.Fprintf(c, "220 \r\n")
	//被动模式套接字
	var dataSock net.Conn
	//重命名记录
	var rnfr string
	for {
		b, _, err := r.ReadLine()
		if err != nil {
			log.Println(err)
			return
		}
		cmd := strings.Split(string(b), " ")
		log.Println(cmd)
		switch strings.ToUpper(cmd[0]) {
		//用户登陆
		case "USER", "PASS":
			fmt.Fprintf(c, "230 \r\n")
		//退出命令
		case "QUIT":
			fmt.Fprintf(c, "221 \r\n")
			return

		//一些配置命令
		case "TYPE", "OPTS", "NOOP":
			fmt.Fprintf(c, "200 \r\n")

		//当前目录
		case "PWD":
			fmt.Fprintf(c, "257 \"%s\"\r\n", "/")

		//切换目录
		case "CWD":
			if cmd[1] != "/" {
				fmt.Fprintf(c, "550 \r\n")
			} else {
				fmt.Fprintf(c, "200 \r\n")
			}

		//删除文件
		case "DELE":
			p.Delete(nil, cmd[1])
			fmt.Fprintf(c, "200 \r\n")

		//重命名
		case "RNFR":
			rnfr = cmd[1]
			fmt.Fprintf(c, "350 \r\n")
		case "RNTO":
			p.Move(nil, rnfr, cmd[1])
			fmt.Fprintf(c, "200 \r\n")

		//文件大小
		case "SIZE":
			entry, err := p.Stat(nil, cmd[1][1:])
			if err != nil {
				fmt.Fprintf(c, "550 \r\n")
				break
			}
			fmt.Fprintf(c, "213 %d\r\n", entry.Fsize)

		//创建目录
		case "MKD":
			fmt.Fprintf(c, "550 Permission denied.\r\n")

		//主动模式
		case "PORT":
			addr := strings.Split(cmd[1], ",")
			ip := strings.Join(addr[:4], ".")
			log.Println(addr)
			portA, err := strconv.Atoi(addr[4])
			if err != nil {
				log.Println(err)
				return
			}
			portB, err := strconv.Atoi(addr[5])
			if err != nil {
				log.Println(err)
				return
			}
			dataSock, err = net.Dial("tcp", fmt.Sprintf("%s:%d", ip, portA*256+portB))
			if err != nil {
				log.Println(err)
			}
			fmt.Fprintf(c, "200 PORT command successful. \r\n")

		//被动模式
		case "PASV", "EPSV":
			s, err := net.Listen("tcp", ":")
			if err != nil {
				log.Panic(err)
			}
			defer s.Close()
			addr := strings.Split(s.Addr().String(), ":")
			port, err := strconv.Atoi(addr[len(addr)-1])
			if err != nil {
				log.Panic(err)
			}

			if cmd[0] == "PASV" {
				fmt.Fprintf(c, "227 Entering Passive Mode (127,0,0,1,%d,%d)\r\n", port/256, port%256)
			} else {
				fmt.Fprintf(c, "229 Extended Passive mode OK (|||%d|)\r\n", port)
			}

			dataSock, err = s.Accept()
			if err != nil {
				log.Panic(err)
			}

		//文件列表
		case "LIST":
			fmt.Fprintf(c, "150 \r\n")

			list, _, _, err := p.List(nil, "", "", "", 100)
			if list == nil {
				log.Panic(err)
			}
			for _, f := range list {
				fmt.Fprintf(dataSock, "%s %d %s\r\n", time.Unix(0, f.PutTime*100).Format("01-02-06 15:04"), f.Fsize, f.Key)
			}

			dataSock.Close()
			fmt.Fprintf(c, "226 \r\n")

		//下载文件
		case "RETR":
			fmt.Fprintf(c, "150 \r\n")

			fName := path.Base(cmd[1])
			pURL := k.MakePrivateUrl(kodo.MakeBaseUrl(conf["Domain"], fName), nil)
			resp, err := http.Get(pURL)
			if err != nil {
				log.Println(err)
				return
			}
			io.Copy(dataSock, resp.Body)
			resp.Body.Close()
			dataSock.Close()
			fmt.Fprintf(c, "226 \r\n")

		//上传文件
		case "STOR":
			fmt.Fprintf(c, "150 \r\n")

			fName := path.Base(cmd[1])
			b, err := ioutil.ReadAll(dataSock)
			if err != nil {
				log.Println(err)
				return
			}
			log.Println(len(b))
			err = p.Put(nil, nil, fName, bytes.NewReader(b), int64(len(b)), nil)
			if err != nil {
				log.Println(err)
				return
			}
			dataSock.Close()
			fmt.Fprintf(c, "226 \r\n")
		default:
			fmt.Fprintf(c, "500 undefined\r\n")
		}
	}
}
