package server

import (
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestOpen(t *testing.T) {
	a := require.New(t)
	tmp := os.Getenv("TEST_TMP")
	a.NotEmpty(tmp, "Set env variable TEST_TMP for tmp dir")
	defer syscall.Unlink(tmp+ "/open.log")

	log, err := OpenFile(tmp + "/open.log", 0600, 10)
	a.Nil(err)
	a.NotNil(log)

	a.FileExists(tmp + "/open.log")

	log2, err := OpenFile(tmp + "/open.log", 0600, 10)

	a.Nil(err)
	a.NotNil(log2)

	info, err := os.Stat(tmp + "/open.log")
	a.Nil(err)
	a.NotNil(info)

	a.Equal(os.FileMode(0600), info.Mode() & 0777)

	log.Close()
	<-log.Done()
}

func TestWriting(t *testing.T) {
	a := require.New(t)
	tmp := os.Getenv("TEST_TMP")
	a.NotEmpty(tmp, "Set env variable TEST_TMP for tmp dir")
	defer syscall.Unlink(tmp +  "/writing.log")

	log, err := OpenFile(tmp + "/writing.log", 0600, 2)
	a.Nil(err)
	a.NotNil(log)

	example := LogMessage{
		Time: time.Unix(1234567890, 0),
		RemoteIp: "127.0.9.10",
		User: "Bzick",
		SSL: "TLS/1.3",
		Method: "GET",
		Host: "example.com",
		URI: "/index.html?cache=false",
		Protocol: "HTTP/2.0",
		UserAgent: "unittest",
		Status: 200,
		BytesSent: 1000,
		Duration: 200 * time.Millisecond,
		RequestId: "D102030",
	}

	log.WriteString("[00:00:00] log message as string")
	log.WriteJSON(example)
	log.WriteNginxLog(example)
	log.WriteNginx2Log(example)

	log.Close()
	<-log.Done()

	data, err := ioutil.ReadFile(tmp +  "/writing.log")
	a.Nil(err)
	a.NotNil(data)

	a.Equal(strings.Join([]string{
		`[00:00:00] log message as string`,
		`{"Time":"2009-02-14T02:31:30+03:00","RemoteIp":"127.0.9.10","User":"Bzick","SSL":"TLS/1.3","Method":"GET","Host":"example.com","URI":"/index.html?cache=false","Protocol":"HTTP/2.0","Referer":"","UserAgent":"unittest","Status":200,"BytesSent":1000,"Duration":200000000,"RequestId":"D102030"}`,
		`127.0.9.10 example.com Bzick [14/Feb/2009:02:31:30 +0300] "GET /index.html?cache=false HTTP/2.0" 200 1000 "-" "unittest"`,
		`127.0.9.10 example.com Bzick [14/Feb/2009:02:31:30 +0300] "GET /index.html?cache=false HTTP/2.0" 200 1000 "-" "unittest" ssl=TLS/1.3 0.200 D102030`,
	}, "\n") + "\n", string(data))

}


func BenchmarkAccessLog(b *testing.B) {
	a := require.New(b)
	tmp := os.Getenv("TEST_TMP")
	a.NotEmpty(tmp, "Set env variable TEST_TMP for tmp dir")
	defer syscall.Unlink(tmp +  "/bench01.log")

	log, err := OpenFile(tmp + "/bench01.log", 0600, 1000)
	a.Nil(err)
	a.NotNil(log)

	for i := 0; i < b.N; i++ {
		go func() {
			log.WriteString(`127.0.9.10 example.com Bzick [14/Feb/2009:02:31:30 +0300] "GET /index.html?cache=false HTTP/2.0" 200 1000 "-" "unittest"` + " ")
		}()
	}
}

func BenchmarkPlainFileWithLock(b *testing.B) {
	a := require.New(b)
	tmp := os.Getenv("TEST_TMP")
	a.NotEmpty(tmp, "Set env variable TEST_TMP for tmp dir")
	defer syscall.Unlink(tmp +  "/bench01-plain.log")

	file, err := os.OpenFile(tmp +  "/bench01-plain.log", os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	a.Nil(err)
	a.NotNil(file)
	mu := sync.Mutex{}
	for i := 0; i < b.N; i++ {
		go func() {
			mu.Lock()
			_, err = file.Write([]byte(`127.0.9.10 example.com Bzick [14/Feb/2009:02:31:30 +0300] "GET /index.html?cache=false HTTP/2.0" 200 1000 "-" "unittest"` + "\n"))
			if err != nil {
				b.Fatal(err)
			}
			mu.Unlock()
		}()
	}
}

func BenchmarkPlainFileWithoutLock(b *testing.B) {
	a := require.New(b)
	tmp := os.Getenv("TEST_TMP")
	a.NotEmpty(tmp, "Set env variable TEST_TMP for tmp dir")
	defer syscall.Unlink(tmp +  "/bench01-plain.log")

	file, err := os.OpenFile(tmp +  "/bench01-plain.log", os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	a.Nil(err)
	a.NotNil(file)
	for i := 0; i < b.N; i++ {
		go func() {
			_, err = file.Write([]byte(`127.0.9.10 example.com Bzick [14/Feb/2009:02:31:30 +0300] "GET /index.html?cache=false HTTP/2.0" 200 1000 "-" "unittest"` + "\n"))
			if err != nil {
				b.Fatal(err)
			}
		}()
	}
}