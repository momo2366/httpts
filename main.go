package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"time"
	// "syscall"

	"github.com/godbus/dbus"
	"github.com/godbus/dbus/introspect"
	"golang.org/x/net/proxy"
)

var printOnly bool
var skipSet bool
var skipdbus bool
var proxyUrl string
var targetUrl string
var minTime time.Time

const (
	minInterval    = 60
	maxInterval    = 300
	defaultUrl     = "https://www.baidu.com"
	requestTimeout = 30
)

const intro = `
<node>
    <interface name="com.kylin.SelfService">
        <method name="SyncTime">
			<arg direction="in" type="s"/>
            <arg direction="out" type="i"/>
        </method>
		<signal name="SyncRes">
			<arg type="i" name="res" direction="out"/>
		</signal>
    </interface>` + introspect.IntrospectDataString + `</node> `

/*
	init proxy
*/
func prepareProxyTransport(proxyUrl string) (*http.Transport, error) {
	var dialer proxy.Dialer

	dialer = proxy.Direct

	if proxyUrl != "" {
		u, err := url.Parse(proxyUrl)
		if err != nil {
			return nil, err
		}

		dialer, err = proxy.FromURL(u, dialer)
		if err != nil {
			return nil, err
		}
	}

	transport := &http.Transport{Dial: dialer.Dial}
	return transport, nil
}

/*
	check local time
	if local time is before minTime,set the minTime
*/

func check_local_time() (err error) {
	now := time.Now()
	if now.Before(minTime) {
		t := minTime.Add(-1 * 60 * 24 * time.Minute)

		log.Printf("minTime time: %s", minTime.UTC())
		log.Printf("System time: %s", now.UTC())

		if !skipSet {
			cmd := exec.Command("timedatectl", "set-ntp", "false") //disable ntp
			err := cmd.Run()
			cmd = exec.Command("bash", "-c", "timedatectl "+"set-time "+minTime.Format("'2006-01-02 15:04:05 UTC'"))
			log.Printf("timedatectl set-time %s", minTime.Format("'2006-01-02 15:04:05 UTC'"))
			err = cmd.Run() //set system clock

			if err != nil {
				log.Printf("Failed to set system clock: %v", err)
				send_signal(1)
				return err
			}
			cmd = exec.Command("timedatectl", "set-local-rtc", "0") //set hardware clock
			err = cmd.Run()
			if err != nil {
				log.Printf("Failed to set hardware clock: %v", err)
			}
			if now.Before(t) {
				send_signal(2)
			} else {
				send_signal(0)
			}
		}
	} else {
		send_signal(1)
	}
	return err
}

/*
	request targetUrl HTTP head and get DATE field
*/
func fetchTime(proxyUrl string, targetUrl string) (parsed time.Time, err error) {

	//init proxy
	transport, err := prepareProxyTransport(proxyUrl)
	if err != nil {
		return
	}

	// init http client
	client := &http.Client{
		Transport: transport,
		Timeout:   requestTimeout * time.Second,
	}

	log.Printf("Start request to %q", targetUrl)

	// Get http resp
	resp, err := client.Get(targetUrl)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	// parse date
	dateHeader := resp.Header.Get("Date")
	parsed, err = time.Parse("Mon, 02 Jan 2006 15:04:05 MST", dateHeader)
	if err != nil {
		return
	}

	// if http date is before minTime,return
	if parsed.Before(minTime) {
		err = errors.New("Timestamp from server is below minimum.")
		return
	}

	return
}

/*
	send dbus signal
	0: Success
	1: Failed
	2: reboot
*/

func send_signal(res int) {
	if !skipdbus {
		conn, err := dbus.SystemBus()
		if err != nil {
			panic(err)
		}
		fmt.Printf("try to send signal %d\n", res)
		conn.Emit("/com/kylin/SelfService", "com.kylin.SelfService.SyncRes", res) //emit signal
	}
	return
}

/*
	start sync system time
*/

func start_sync() {
	if printOnly {
		fetched, err := fetchTime(proxyUrl, targetUrl)
		if err != nil {
			log.Fatalf("%v", err)
		}

		fmt.Printf("%s\n", fetched.UTC())
		send_signal(0)
		return
	}

	//TODO:Clock Filter Algorithm

	fetched, err := fetchTime(proxyUrl, targetUrl)
	if err == nil {
		now := time.Now()
		t := fetched.Add(-1 * 60 * 24 * time.Minute)
		offset := fetched.Sub(now)

		log.Printf("Remote time: %s", fetched.UTC())
		log.Printf("System time: %s", now.UTC())
		log.Printf("Remote offset from system clock: %v", offset)

		if !skipSet {
			//use command : date -s
			// cmd := exec.Command("date","-s",fetched.Format("2006-01-02 15:04:05 UTC"))
			// log.Printf("date -s %s",fetched.Format("2006-01-02 15:04:05 UTC"))

			//use command : timedatectl set-time
			cmd := exec.Command("timedatectl", "set-ntp", "false") //disable ntp
			err := cmd.Run()
			cmd = exec.Command("bash", "-c", "timedatectl "+"set-time "+fetched.Format("'2006-01-02 15:04:05 UTC'"))
			log.Printf("timedatectl set-time %s", fetched.Format("'2006-01-02 15:04:05 UTC'"))
			err = cmd.Run() //set system clock

			//use syscall : adjtimex
			// ADJ_OFFSET :Since Linux 2.6.26, the supplied value is clamped to the range (-0.5s, +0.5s)
			// state, err := syscall.Adjtimex(&syscall.Timex{
			// 	Modes:  1, // ADJ_OFFSET = 1
			// 	Offset: int64(offset / time.Microsecond),
			// })
			// if state != 0 {
			// 	log.Printf("Return value of adjtime call is nonzero: %v", state)
			// 	send_signal(1)
			// 	return
			// }

			if err != nil {
				log.Printf("Failed to set system clock: %v", err)
				send_signal(1)
				return
			}

			cmd = exec.Command("timedatectl", "set-local-rtc", "0") //set hardware clock
			err = cmd.Run()
			if err != nil {
				log.Printf("Failed to set hardware clock: %v", err)
			}

			if now.Before(t) {
				send_signal(2)
			} else {
				send_signal(0)
			}
		}
	} else {
		log.Printf("Error fetching time: %v,try to check local time", err)
		check_local_time()
		return
	}

}

type dbus_server struct {
}

func (s dbus_server) SyncTime(tarurl string) (int, *dbus.Error) {
	fmt.Printf("try to GET %s: DATE\n", tarurl)
	targetUrl = tarurl
	go start_sync()
	return 0, nil
}

func (s dbus_server) SyncRes(res int) (int, *dbus.Error) {
	return res, nil
}

func main() {

	flag.BoolVar(&printOnly, "printonly", false, "Print the time and immediately exit")
	flag.BoolVar(&skipSet, "skipset", false, "Don't try to set the system clock")
	flag.BoolVar(&skipdbus, "skipdbus", false, "Don't try to export the dbus server")
	flag.StringVar(&proxyUrl, "proxy", "", "URL of proxy used to access the server")
	flag.StringVar(&targetUrl, "url", defaultUrl, "URL to an HTTP server with an accurate Date header")
	flag.Parse()

	minTime = time.Date(2018, 7, 19, 0, 0, 0, 0, time.UTC)

	if skipdbus {
		start_sync()
		os.Exit(0)
	}

	conn, err := dbus.SystemBus()
	if err != nil {
		panic(err)
	}
	reply, err := conn.RequestName("com.kylin.SelfService",
		dbus.NameFlagDoNotQueue)
	if err != nil {
		panic(err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		fmt.Fprintln(os.Stderr, "name already taken")
		os.Exit(1)
	}
	s := dbus_server{}
	conn.Export(s, "/com/kylin/SelfService", "com.kylin.SelfService")
	conn.Export(introspect.Introspectable(intro), "/com/kylin/SelfService",
		"org.freedesktop.DBus.Introspectable")
	fmt.Println("Listening on com.kylin.SelfService / /com/kylin/SelfService ...")
	select {}
}
