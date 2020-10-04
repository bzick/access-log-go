package server

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"
)

const BufferSize = 256
const MaxBufferSize = 512

const Delimiter = 0x0A // \n

const StrategyIgnore = 0
const StrategyWait = 1

type LogMessage struct {
	// Local time
	Time time.Time
	// Remote IP
	RemoteIp string
	// User name
	User string
	// SSL version: TLS/1.2, TLS/1.3. Empty string if SSL not used.
	SSL string
	// HTTP method: GET, POST, PUT
	Method string
	Host string
	URI string
	// HTTP protocol version
	Protocol string
	Referer string
	UserAgent string
	// Response status
	Status int
	// The number of bytes sent to a client
	BytesSent int64
	// Request processing time
	Duration time.Duration
	RequestId string
}

type AccessLog struct {
	Error error

	pool	 sync.Pool
	ch       chan []byte
	filePath string
	doneCh   chan bool
	reopenCh chan bool
	closeCh  chan bool
	file 	 *os.File
	perm 	 os.FileMode

	writeStrategy uint8

	retries uint8
	wait time.Duration
}

type Buffer struct {
	Buf []byte
}

func (b *Buffer) Write(data []byte) (int, error) {
	b.Buf = append(b.Buf, data...)
	return len(data), nil
}

func OpenFile(filePath string, perm os.FileMode, bufferSize uint) (*AccessLog, error) {
	var (
		err error
		acc = &AccessLog{}
	)

	acc.writeStrategy = StrategyWait
	acc.filePath = filePath
	acc.ch = make(chan []byte, bufferSize)
	acc.closeCh = make(chan bool, 1)
	acc.doneCh = make(chan bool, 1)
	acc.file, err = os.OpenFile(acc.filePath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, perm)
	if err != nil {
		return nil, fmt.Errorf("Failed to open access log: %s", err)
	}
	acc.pool.New = func() interface{} {
		return make([]byte, 0, BufferSize)
	}
	go acc.write()

	return acc, nil
}

// Set retry count and delay.
// Retry used when write to file fails or reopen file fails
func (acc *AccessLog) SetRetries(count uint8, wait time.Duration) {
	acc.retries = count
	acc.wait = wait
}

// Err method returns fatal error
func (acc *AccessLog) Err() error {
	return acc.Error
}

// A go-routine that works with the log file.
// When the go-routine is finished, it sends a message to the doneCh channel.
// To stop go-routine send message to the closeCh channel.
// If an error occurs then the Error field is set
func (acc *AccessLog) write() {
	var err error
	defer close(acc.doneCh)
	for {
		select {
		case logMessage := <-acc.ch:
			retries := acc.retries
			for {
				if _, err := acc.file.Write(logMessage); err != nil {
					if retries > 0 {
						log.Printf("waiting")
						retries--
						time.Sleep(acc.wait)
						continue
					} else {
						acc.Error = fmt.Errorf("Failed to write to log file %s: %s", acc.filePath, err)
						acc.doneCh <- false
						return
					}
				} else if cap(logMessage) <= MaxBufferSize {
					logMessage = logMessage[:0]
					acc.pool.Put(logMessage)
				}

				break
			}
		case <-acc.reopenCh:
			_ = acc.file.Close()
			retries := acc.retries
			for {
				acc.file, err = os.OpenFile(acc.filePath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, acc.perm)
				if err != nil {
					if retries > 0 {
						retries--
						time.Sleep(acc.wait)
						continue
					} else {
						acc.Error = fmt.Errorf("Failed to reopen log file %s: %s", acc.filePath, err)
						acc.doneCh <- false
						return
					}
				}
			}
		case <-acc.closeCh:
			close(acc.ch)
			if acc.file != nil {
				for logMessage := range acc.ch {
					if _, err := acc.file.Write(logMessage); err != nil {
						acc.Error = fmt.Errorf("Failed to write to log file %s: %s", acc.filePath, err)
						break
					}
				}
				_ = acc.file.Close()
			}
			if err != nil {
				acc.Error = err
				acc.doneCh <- false
			} else {
				acc.doneCh <- true
			}
			return
		}
	}
}

func (acc *AccessLog) Done() <-chan bool {
	return acc.doneCh
}

func (acc *AccessLog) ReOpen() {
	acc.reopenCh <- true
}

func (acc *AccessLog) send(message []byte) bool {
	if acc.writeStrategy == StrategyWait {
		acc.ch <- message
		return true
	} else {
		select {
		case acc.ch <- message:
			return true
		default:
			// we has a problem
			if cap(message) <= MaxBufferSize {
				message = message[:0]
				acc.pool.Put(message)
			}
			return false
		}
	}
}

// Writes a string to the log.
// New line (\n) will be automatically added to the end of the message.
func (acc *AccessLog) WriteString(message string) bool  {
	var buf []byte
	if len(message) + 1 <= MaxBufferSize {
		buf = acc.pool.Get().([]byte)[:len(message) + 1]
	} else {
		buf = make([]byte, len(message) + 1)
	}
	copy(buf, message)
	buf[len(buf) - 1] = Delimiter
	return acc.send(buf)
}

// Writes JSON message.
// The message argument marshaling into JSON and writes to log file.
func (acc *AccessLog) WriteJSON(message interface{}) bool {
	buf := acc.pool.Get().([]byte)
	b := Buffer{
		Buf: buf,
	}
	if err := json.NewEncoder(&b).Encode(message); err != nil {
		if cap(buf) <= MaxBufferSize {
			buf = buf[:0]
			acc.pool.Put(buf)
		}
		return false
	}
	return acc.send(b.Buf)
}

// Writes Nginx-like access log. Example:
//  127.0.9.10 example.com - [14/Feb/2009:02:31:30 +0300] "GET /index.html?cache=false HTTP/2.0" 200 1000 "-" "unittest"
func (acc *AccessLog) WriteNginxLog(message LogMessage) bool {
	if message.RemoteIp == "" {
		message.RemoteIp = "-"
	}
	if message.Host == "" {
		message.Host = "-"
	}
	if message.User == "" {
		message.User = "-"
	}
	if message.Referer == "" {
		message.Referer = "-"
	}
	if message.UserAgent == "" {
		message.UserAgent = "-"
	}
	// we don't use fmt because reflect does too much computation
	return acc.WriteString(message.RemoteIp +
		" " + message.Host +
		" " + message.User +
		" [" + message.Time.Format("02/Jan/2006:15:04:05 -0700") + "]" +
		" \"" + message.Method + " " + message.URI + " " + message.Protocol + "\"" +
		" " + strconv.Itoa(message.Status) +
		" " + strconv.FormatInt(message.BytesSent, 10) +
		" \"" + message.Referer + "\"" +
		" \"" + message.UserAgent + "\"",
		)
}


// Extended log of WriteNginxLog. Additionally writes request duration, SSL info and request UID:
//  127.0.9.10 example.com - [14/Feb/2009:02:31:30 +0300] "GET /index.html?cache=false HTTP/2.0" 200 1000 "-" "unittest" ssl=TLS/1.3 0.200 D102030
func (acc *AccessLog) WriteNginx2Log(message LogMessage) bool {
	if message.RemoteIp == "" {
		message.RemoteIp = "-"
	}
	if message.Host == "" {
		message.Host = "-"
	}
	if message.User == "" {
		message.User = "-"
	}
	if message.Referer == "" {
		message.Referer = "-"
	}
	if message.UserAgent == "" {
		message.UserAgent = "-"
	}
	if message.SSL == "" {
		message.SSL = "-"
	}
	if message.RequestId == "" {
		message.RequestId = "-"
	}
	// we don't use fmt because reflect does too much computation
	return acc.WriteString(message.RemoteIp +
		" " + message.Host +
		" " + message.User +
		" [" + message.Time.Format("02/Jan/2006:15:04:05 -0700") + "]" +
		" \"" + message.Method + " " + message.URI + " " + message.Protocol + "\"" +
		" " + strconv.Itoa(message.Status) +
		" " + strconv.FormatInt(message.BytesSent, 10) +
		" \"" + message.Referer + "\"" +
		" \"" + message.UserAgent + "\"" +
		" ssl=" + message.SSL +
		" " + strconv.FormatFloat(message.Duration.Seconds(), 'f', 3, 64) +
		" " + message.RequestId,
	)
}

func (acc *AccessLog) Close() {
	acc.closeCh <- true
}