package client

import (
	"bytes"
	"fmt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

//做个http client从固定的网站下载文件，支持断点续传，下载的文件保存在当前目录
//cross over: 339.32M
//var durl = "http://downza.91speed.vip/2022/04/21/crossover.zip"

//4.4G
//var durl = "http://officecdn.microsoft.com/pr/492350f6-3a01-4f97-b9c0-c7c6ddf67d60/media/zh-cn/ProPlus2019Retail.img"

func printRespInfo(resp *http.Response) {
	fmt.Println("Response Status:=", resp.Status)
	headers := resp.Header
	for header := range headers {
		v := headers[header]
		println(header + "=" + strings.Join(v, "|"))
	}
}

//实现goroutine处理大文件下载
var Dir string

func DownloadFileGo(durl string) (err error) {
	srcUrl, err := url.ParseRequestURI(durl)
	if err != nil {
		panic("ParseRequestURI failure!")
	}
	filename := path.Base(srcUrl.Path)

	//发送HTTP HEAD，看server是否支持Range
	myClient := http.Client{}
	req, err := http.NewRequest(http.MethodHead, durl, nil)
	if err != nil {
		panic("http.NewRequest HEAD failure..")
	}
	req.Header.Add("Range", "bytes=0-0")
	resp, err := myClient.Do(req)
	if err != nil {
		panic("myClient.Do: HTTP HEAD send failure.." + durl)
	}
	defer resp.Body.Close()

	//print resp headers
	//printRespInfo(resp)

	//make the destination file directory
	if len(Dir) != 0 {
		err = os.Chdir(Dir)
		if err != nil {
			err = os.MkdirAll(Dir, 0777)
			if err != nil {
				fmt.Printf("make directory %s failure..\n", Dir)
				return err
			}
		}
	}

	//resp header
	v := resp.Header.Get("Accept-Ranges")
	if v != "bytes" {
		downloadFileNoRange(Dir+filename, durl)
		return nil
	}

	//取得文件大小
	var total int64
	contentRange := resp.Header.Get("Content-Range")
	totalRange := strings.Split(contentRange, "/")
	if len(totalRange) >= 2 {
		total, _ = strconv.ParseInt(totalRange[1], 10, 64)
	}

	//用channel实现, 并发处理下载文件
	downloadFileGoroutine(Dir+filename, total, durl)
	return nil

}

//不支持断点下传的服务器
func downloadFileNoRange(filename string, url string) {
	ntStart := time.Now()
	file1, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY, os.ModePerm)
	if err != nil {
		panic("Open file failure..")
	}
	defer file1.Close()

	resp, err := http.Get(url)
	if err != nil {
		panic("http.Get failure..")
	}
	defer resp.Body.Close()
	v := resp.Header.Get("Content-Length")
	contentLength, _ := strconv.ParseInt(v, 10, 64)

	//分片存储到文件
	n := 0
	buf := make([]byte, 1024*1024)
	flag := 0
	for {
		num, err := resp.Body.Read(buf)
		if err != nil {
			if err == io.EOF {
				//break
				fmt.Println("resp.Body.Read EOF...")
			} else {
				fmt.Println("resp.Body.Read failure")
			}
			flag = 1
		}

		file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY, os.ModePerm)
		file.Seek(int64(n), io.SeekStart)
		num, err = file.Write(buf[:num])
		n += num

		if flag == 1 {
			break
		}
	}
	//分片存储到文件

	if n != int(contentLength) {
		fmt.Println("The file size maybe wrong...")
	}

	ntEnd := time.Now()
	fmt.Printf("共用时：%v\n", ntEnd.Sub(ntStart))
}

//////////////////////////////////////////
//用channel实现多协程下载
/////////////////////////////////////////

type recorder interface {
	write()
	read() ([]Range, error)
}

type Range struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

type RangeData struct {
	ID     int   `gorm:"primaryKey"`
	RangeD Range `gorm:"embedded"`
}

var chanRange chan Range
var ChanDisc chan Range
var ChanSig chan struct{}

//var eachRangeLen int64
var wg = sync.WaitGroup{}
var RangeSize int64
var Goroutines int
var ChanCnt int64

func downloadFileGoroutine(filename string, size int64, url string) {
	ntStart := time.Now()

	if RangeSize == 0 {
		RangeSize = 1024 * 1024 * 10 //10M
	}

	if Goroutines == 0 {
		Goroutines = 10
	}
	if ChanCnt == 0 {
		ChanCnt = 100
	}

	var rwLock sync.RWMutex

	for {
		file, err := os.Open(filename)
		if err != nil {
			HandleError(err, "open file failure..")
		}
		info, err := file.Stat()
		if err == nil {
			if info.Size() >= size { //文件大小已经下载完毕
				fmt.Println("文件下载完毕！")
				break
			}
		}
		file.Close()

		// 1.初始化管道
		chanRange = make(chan Range, ChanCnt)
		ChanDisc = make(chan Range, ChanCnt)
		ChanSig = make(chan struct{}, Goroutines)

		//启动多个协程下载文件
		for i := 0; i < Goroutines; i++ {
			//wg.Add(1)
			go DownloadFileRange(&rwLock, url, filename, size)
		}
		fmt.Printf("共启动%d个协程...\n", Goroutines)

		//数据库的写入有个线程
		wg.Add(1)
		//go WriteTheDownloadDesc()
		var r RangeData
		go r.write()

		//把range切片,放进channel
		SliceTheRanges(size)
		//SliceSizeToRange(0, size)
		close(chanRange)

		//
		cnt := 0
		for _ = range ChanSig {
			cnt++
			if cnt >= Goroutines {
				close(ChanDisc)
				fmt.Printf("the %d goroutines done and chanDisc is closed...\n", Goroutines)
				break
			}
		}

		wg.Wait()
	}

	ntEnd := time.Now()
	fmt.Printf("共用时：%v\n", ntEnd.Sub(ntStart))

	//打印一下下载的文件大小是否一致
	file, _ := os.Open(filename)
	info, _ := file.Stat()
	fmt.Printf("下载的文件大小：%d/%d\n", info.Size(), size)
	file.Close()
}

func DownloadFileRange(rwLock *sync.RWMutex, url string, filename string, filesize int64) {

	for rangeGet := range chanRange {
		myClient := &http.Client{}
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			HandleError(err, "http.NewRequest")
			continue
		}
		if rangeGet.End == 0 {
			req.Header.Add("Range", fmt.Sprintf("bytes=%d-", rangeGet.Start))
		} else {
			req.Header.Add("Range", fmt.Sprintf("bytes=%d-%d", rangeGet.Start, rangeGet.End))
		}

		resp, err := myClient.Do(req)
		if err != nil {
			resp, err = myClient.Do(req)
		}
		if err != nil {
			HandleError(err, "myClient.Do")
			continue
		}

		//分片存储到文件
		seekStart := rangeGet.Start
		n := 0
		buf := make([]byte, 1024*1024)
		flag := 0
		for {
			num, err := resp.Body.Read(buf)
			if err != nil {
				if err == io.EOF {
					//fmt.Println("resp.Body.Read EOF...")
				} else {
					fmt.Println("resp.Body.Read failure")
				}
				flag = 1
			}

			rwLock.Lock()
			file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY, os.ModePerm)
			if err != nil {
				HandleError(err, "os.OpenFile filename")

				continue
			}
			file.Seek(seekStart, io.SeekStart)
			num, err = file.Write(buf[:num])
			file.Close()
			rwLock.Unlock()
			n += num
			seekStart += int64(num)
			HandleError(err, "file write")

			if flag == 1 {
				break
			}
		}
		resp.Body.Close()
		//分片存储到文件

		//打印下载信息
		file, _ := os.Open(filename)
		info, _ := file.Stat()
		fmt.Printf("goroutine %d :\n,", GetGID())
		fmt.Printf("This download Range:%d-%d, The download filesize %d/%d\n", rangeGet.Start, rangeGet.Start+int64(n)-1, info.Size(), filesize)

		file.Close()

		//在数据库记录下，下载情况
		ChanDisc <- Range{rangeGet.Start, rangeGet.Start + int64(n) - 1}
	}
	//wg.Done()

	ChanSig <- struct{}{}
}

func HandleError(err error, why string) {
	if err != nil {
		fmt.Println(why, err)
	}
}

func SliceSizeToRange(rangeStart int64, rangeEnd int64) {
	var start int64 = rangeStart
	var end int64 = 0
	var n int = 0
	for {
		if start > rangeEnd {
			fmt.Printf("共切了%d片\n", n)
			break
		}

		end = start + RangeSize - 1
		if end > rangeEnd {
			end = rangeEnd
		}
		chanRange <- Range{start, end}

		start = end + 1
		n++
	}

}

func GetGID() uint64 {
	b := make([]byte, 64)
	b = b[:runtime.Stack(b, false)]
	b = bytes.TrimPrefix(b, []byte("goroutine "))
	b = b[:bytes.IndexByte(b, ' ')]
	n, _ := strconv.ParseUint(string(b), 10, 64)
	return n
}

func SliceTheRanges(filesize int64) {

	//从数据库读取range scope
	var rangeSlice []Range
	//rangeSlice, err := ReadTheDownloadDesc()
	var r RangeData
	rangeSlice, err := r.read()
	if err != nil {
		fmt.Println("read database failure...")
		SliceSizeToRange(0, filesize)
		return
	}

	//sort the range
	//从数据库取出来的时候已经排序了

	var i int

	for i = 1; i < len(rangeSlice); i++ {
		if rangeSlice[i].Start > rangeSlice[i-1].End+1 {
			SliceSizeToRange(rangeSlice[i-1].End+1, rangeSlice[i].Start-1)
		}
	}
	v := rangeSlice[i-1].End
	if v != 0 && v < filesize {
		SliceSizeToRange(v+1, filesize)
	}
	return
}

//func WriteTheDownloadDesc() {
func (r RangeData) write() {
	dsn := Dir + "downloader.db"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		fmt.Println("open downloader db failure...")
		return
	}

	db.AutoMigrate(&RangeData{})

	for rangeGet := range ChanDisc {
		//db.Create(&RangeData{Start: rangeGet.Start, End: rangeGet.End})
		db.Create(&RangeData{RangeD: rangeGet})
		fmt.Printf("Insert the range to the database...Range is %d-%d\n", rangeGet.Start, rangeGet.End)
	}
	wg.Done()
}

//func ReadTheDownloadDesc() (rangeArr []Range, err error) {
func (r RangeData) read() (rangeArr []Range, err error) {
	dsn := Dir + "downloader.db"
	_, err = os.Open(dsn)
	if err != nil {
		return nil, err
	}
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		fmt.Println("open downloader db failure...")
		return nil, err
	}

	db.AutoMigrate(&RangeData{})

	rangeData := make([]RangeData, 1024)
	result := db.Order("Start").Find(&rangeData)
	if result.Error != nil {
		return nil, result.Error
	}
	ranges := make([]Range, result.RowsAffected)
	for i := 0; int64(i) < result.RowsAffected; i++ {
		ranges[i] = rangeData[i].RangeD
	}

	return ranges, nil
}
