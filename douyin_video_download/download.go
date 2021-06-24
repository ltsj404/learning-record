package main

import (
	"bufio"
	"flag"
	"fmt"
	"github.com/cilidm/toolbox/file"
	"github.com/dustin/go-humanize"
	"github.com/kirinlabs/HttpRequest"
	"github.com/tidwall/gjson"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var IsYear bool

func init() {
	flag.BoolVar(&IsYear, "y", false, "是否按年归类")
}

/**
1.添加 url.txt 文件 用户路径，一个用户占一行
*/
func main() {
	flag.Parse()
	SpiderDY()
}

// --------------spider----------------
var (
	req         *HttpRequest.Request
	downloadDir = "download"
)

func init() {
	req = HttpRequest.NewRequest()
	req.SetHeaders(map[string]string{
		"User-Agent": "Mozilla/5.0 (Linux; Android 6.0; Nexus 5 Build/MRA58N) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/89.0.4389.114 Mobile Safari/537.36",
	})
	req.CheckRedirect(func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse /* 不进入重定向 */
	})
}

// 爬取文件
func SpiderDY() {
	lines, err := ReadLine("url.txt")
	if err != nil {
		os.Create("url.txt")
		log.Fatal("未找到url.txt文件，已自动创建，请在同目录下url.txt文件加入分享链接，每个分享链接一行")
	}
	for _, line := range lines {
		reg := regexp.MustCompile(`[a-z]+://[\S]+`)
		url := reg.FindAllString(line, -1)[0]
		resp, err := req.Get(url)
		defer resp.Close()
		if err != nil {
			fmt.Errorf(err.Error())
			continue
		}
		if resp.StatusCode() != 302 {
			continue
		}
		location := resp.Headers().Values("location")[0]

		regNew := regexp.MustCompile(`(?:sec_uid=)[a-z,A-Z，0-9, _, -]+`)
		sec_uid := strings.Replace(regNew.FindAllString(location, -1)[0], "sec_uid=", "", 1)

		respIes, err := req.Get(fmt.Sprintf("https://www.iesdouyin.com/web/api/v2/user/info/?sec_uid=%s", sec_uid))
		defer respIes.Close()
		if err != nil {
			fmt.Errorf(err.Error())
			continue
		}
		body, err := respIes.Body()
		result := gjson.Get(string(body), "user_info.nickname").String()
		dirPath := fmt.Sprintf("%s/%s/", downloadDir, result)
		err = file.IsNotExistMkDir(dirPath)
		if err != nil {
			fmt.Errorf(err.Error())
			continue
		}
		GetByMonth(sec_uid, dirPath)
	}
}

func GetByMonth(secUid, dirPath string) {
	y := 2018
	nowY, _ := strconv.Atoi(time.Now().Format("2006"))
	nowM, _ := strconv.Atoi(time.Now().Format("01"))
	// 原始目录路径
	rawDirPath := dirPath
	for i := y; i <= nowY; i++ {
		for m := 1; m <= 12; m++ {
			var (
				begin int64
				end   int64
			)
			if i == nowY && m > nowM {
				break
			}
			begin = GetMonthStartAndEnd(strconv.Itoa(i), strconv.Itoa(m))
			if m == 12 {
				end = GetMonthStartAndEnd(strconv.Itoa(i+1), "1")
			} else {
				end = GetMonthStartAndEnd(strconv.Itoa(i), strconv.Itoa(m+1))
			}
			resp, err := req.Get(fmt.Sprintf("https://www.iesdouyin.com/web/api/v2/aweme/post/?sec_uid=%s&count=200&min_cursor=%d&max_cursor=%d&aid=1128&_signature=PtCNCgAAXljWCq93QOKsFT7QjR",
				secUid, begin, end))
			defer resp.Close()
			if err != nil {
				fmt.Errorf(err.Error())
				continue
			}
			body, err := resp.Body()

			r := gjson.Get(string(body), "aweme_list").Array()
			if len(r) > 0 {
				if IsYear {
					// 原始目录路径 + 年
					dirPath = fmt.Sprintf("%s%d/", rawDirPath, i)
					err = file.IsNotExistMkDir(dirPath)
					if err != nil {
						fmt.Errorf(err.Error())
						continue
					}
				}
				for n := 0; n < len(r); n++ {
					videotitle := gjson.Get(string(body), fmt.Sprintf("aweme_list.%d.desc", n)).String()
					videourl := gjson.Get(string(body), fmt.Sprintf("aweme_list.%d.video.play_addr.url_list.0", n)).String()
					err = DownloadFile(dirPath+videotitle+".mp4", videourl)
					if err != nil {
						fmt.Errorf(err.Error())
						continue
					}
				}
			}
		}
	}
}

// -----------------util----------------------
// 下载文件显示进度条
type WriteCounter struct {
	Name  string
	Total uint64
}

func (wc *WriteCounter) Write(p []byte) (int, error) {
	n := len(p)
	wc.Total += uint64(n)
	wc.PrintProgress()
	return n, nil
}

func (wc WriteCounter) PrintProgress() {
	fmt.Printf("\r%s", strings.Repeat(" ", 35))
	fmt.Printf("\r 【%s】 Downloading... %s complete", wc.Name, humanize.Bytes(wc.Total))
}
func DownloadFile(fileName string, url string) error {
	req, err := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 6.0; Nexus 5 Build/MRA58N) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/89.0.4389.114 Mobile Safari/537.36")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	fileNameSp := strings.Split(fileName, "/")
	if !file.CheckNotExist(fileName) {
		stat, err := os.Stat(fileName)
		if err != nil {
			return err
		}
		newSize := resp.Header.Get("Content-Length")
		if newSize == strconv.FormatInt(stat.Size(), 10) {
			log.Println(fileNameSp[len(fileNameSp)-1], "已存在，跳过")
			return nil
		}
	}

	out, err := os.Create(fileName + ".tmp")
	if err != nil {
		defer out.Close()
		return err
	}
	counter := &WriteCounter{}
	counter.Name = fileNameSp[len(fileNameSp)-1]
	if _, err = io.Copy(out, io.TeeReader(resp.Body, counter)); err != nil {
		out.Close()
		return err
	}
	fmt.Println()
	out.Close()
	if err = os.Rename(fileName+".tmp", fileName); err != nil {
		return err
	}
	return nil
}

//GetMonthStartAndEnd 获取月份的第一天和最后一天
func GetMonthStartAndEnd(myYear string, myMonth string) int64 {
	// 数字月份必须前置补零
	if len(myMonth) == 1 {
		myMonth = "0" + myMonth
	}
	yInt, _ := strconv.Atoi(myYear)

	timeLayout := "2006-01-02 15:04:05"
	loc, _ := time.LoadLocation("Local")
	theTime, _ := time.ParseInLocation(timeLayout, myYear+"-"+myMonth+"-01 00:00:00", loc)
	newMonth := theTime.Month()
	return time.Date(yInt, newMonth, 1, 0, 0, 0, 0, time.Local).UnixNano() / 1e6
}

// 按行读取配置
func ReadLine(fileName string) (lines []string, err error) {
	f, err := os.Open(fileName)
	if err != nil {
		return nil, err
	}
	buf := bufio.NewReader(f)
	for {
		line, err := buf.ReadString('\n')
		line = strings.TrimSpace(line)
		lines = append(lines, line)
		if err != nil {
			if err == io.EOF {
				return lines, nil
			}
			return nil, err
		}
	}
}

func RemoveFiles() {
	files, err := GetDirs("./download/Fairy", ".tmp")
	if err != nil {
		fmt.Println(files, err)
		return
	}
	filesNum := len(files)
	//fmt.Println(strings.Join(files, "\n"))
	fmt.Printf("文件总数: %d\n", filesNum)
	var waitGroup sync.WaitGroup
	for _, v := range files {
		fileName := v
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			os.Remove(fileName)
		}()
	}
	fmt.Println("finish ^^!")
	waitGroup.Wait()
}

// 获取目录下的符合后缀的所有文件
func GetDirs(dirPath, suffix string) (files []string, err error) {
	files = make([]string, 0, 500)
	suffix = strings.ToUpper(suffix)
	err = filepath.Walk(dirPath, func(fileName string, fi os.FileInfo, err error) error {
		if fi.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToUpper(fi.Name()), suffix) {
			files = append(files, fileName)
		}
		return nil
	})
	return files, err
}
