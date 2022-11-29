package main

import (
	"os"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
	"strconv"
	"syscall"
	"context"
	"io/ioutil"
	"os/signal"
	"encoding/json"
	"net/url"
	"net/http"
	"net/http/httputil"
)

type Endpoint struct {
	Url string `json: "Url"`
	Port string `json: "Port"`
	Active bool `json: "Active"`
}

type RoundRobin struct {
	Endpoints []*Endpoint `json: "endpoints"`
	DeadMap map[int]bool
	Length int
	Initialized bool
}

var rr RoundRobin

var CurIndex int

var Mu sync.RWMutex

var Duration int

var AllConnectionDown bool

func init () {
	durStr := os.Getenv("LB_LIVENESS_CHECK_DURATION")
	dur, err := strconv.Atoi(durStr)
	if err != nil {
		log.Fatal(err, "\nstrconv.Atoi failed to convert ENV to int")
	}
	Duration = dur

	pwd := os.Getenv("PWD")
	f, err := os.Open(pwd + "/endpoints.json")
	if err != nil {
		log.Fatal("Couldn't read endpoints. Check that 'endpoints.json' exists in root.")
	}

	b, err := ioutil.ReadAll(f)
	if err != nil {
		log.Fatal("Couldn't convert 'endpoints.json' to bytes.")
	}

	err = json.Unmarshal(b, &rr.Endpoints)
	if err != nil {
		log.Fatal(err)
	}

	// Get PID and write to file for shutdown
	intPid := os.Getpid()
	strPid := strconv.Itoa(intPid)
	err = os.WriteFile(pwd + "/pid.txt", []byte(strPid), 0600)
	if err != nil {
		fmt.Println(err)
	}

	// One time network setup and status print
	rr.NetworkSetup()

	// Perpetual health check in set intervals
	go rr.Controller() 
}


func (rr *RoundRobin) NetworkSetup () {
	rr.DeadMap = make(map[int]bool)

	for i, ep := range rr.Endpoints {
		addr, err := url.Parse(ep.Url + ":" + ep.Port)
		if err != nil {
			log.Fatal(err)
		}
		
		_, err = net.Dial("tcp", addr.Host)
		if err != nil {
			rr.Endpoints[i].Active = false
			rr.DeadMap[i] = true
		} else {
			rr.Endpoints[i].Active = true
		}
	}

	rr.Length = len(rr.Endpoints)
	CurIndex = rr.Length - 1
	
	deadConnections := 0
	fmt.Println("Endpoints:")
	for _, e := range rr.Endpoints {
		msg := "Down"
		if e.Active {
			msg = "Active"
		} else {
			deadConnections++
		}

		fmt.Println(e.Url + ":" + e.Port, msg)
	}

	if deadConnections == rr.Length {
		fmt.Println("ALERT. All connections are down.")
	}
}


func (rr *RoundRobin) Controller () {
	tick := time.NewTicker(time.Second * time.Duration(Duration))
	for {
		<- tick.C 
		rr.CheckEndPoints()
		rr.ShutdownCheck()
	}
}


func (rr *RoundRobin) CheckEndPoints () {
	
	for i, ep := range rr.Endpoints {
		addr, err := url.Parse(ep.Url + ":" + ep.Port)
		if err != nil {
			log.Fatal(err)
		}
		
		_, err = net.Dial("tcp", addr.Host)
		if err != nil {
			rr.SetDead(i)	
		} else {
			rr.SetActive(i)
		}
	}
}


func (rr *RoundRobin) SetActive (i int) {
	active := false
	Mu.RLock()
	active = rr.Endpoints[i].Active
	Mu.RUnlock()

	if active {
		return
	}

	url := ""
	port := ""

	Mu.Lock()
	rr.Endpoints[i].Active = true
	rr.DeadMap[i] = false
	AllConnectionDown = false
	Mu.Unlock()

	Mu.RLock()
	url = rr.Endpoints[i].Url
	port = rr.Endpoints[i].Port
	Mu.RUnlock()

	log.Println("Endpoint:", url + ":" + port, "Active")
}


func (rr *RoundRobin) SetDead (i int) {
	active := false
	Mu.RLock()
	active = rr.Endpoints[i].Active
	Mu.RUnlock()

	if !active {
		return
	}

	url := ""
	port := ""
	
	Mu.Lock()
	rr.Endpoints[i].Active = false
	rr.DeadMap[i] = true
	Mu.Unlock()

	Mu.RLock()
	url = rr.Endpoints[i].Url
	port = rr.Endpoints[i].Port
	Mu.RUnlock()

	log.Println("Endpoint:", url + ":" + port, "Down")
}


func (rr *RoundRobin) ShutdownCheck() {
	deadConnections := 0
	for i := 0; i < rr.Length; i++ {
		Mu.RLock()
		if !rr.Endpoints[i].Active {
			deadConnections++
		}
		Mu.RUnlock()
	}

	if deadConnections == len(rr.Endpoints) {
		Mu.Lock()
		AllConnectionDown = true
		Mu.Unlock()
		log.Println("ALERT. All connections are down.")
	}
}


func (rr *RoundRobin) RotateCurIndex () {
	Mu.Lock()
	for i := CurIndex + 1; i < rr.Length; i++ {
		if !rr.DeadMap[i] {
			CurIndex = i
			Mu.Unlock()
			return
		} 
	}

	for i := 0; i < CurIndex; i++ {
		if !rr.DeadMap[i] {
			CurIndex = i
			Mu.Unlock()
			return
		} 
	}

	Mu.Unlock()
	rr.ShutdownCheck()
}


func reverseProxy (w http.ResponseWriter, r *http.Request) {
	rr.RotateCurIndex()
	
	Mu.RLock()
	if AllConnectionDown {
		Mu.RUnlock()
		w.WriteHeader(502)
		w.Write([]byte("Bad Gateway."))
		return
	}
	
	nextUrl := rr.Endpoints[CurIndex].Url
	nextPort := rr.Endpoints[CurIndex].Port
	Mu.RUnlock()
	
	addr, err := url.Parse(nextUrl + ":" + nextPort)
	if err != nil {
		log.Fatal(err)
	}
	
	// r.URL is currently blank. Copy data from *url.URL.
	r.URL.Host = addr.Host
	r.URL.Scheme = addr.Scheme
	
	// Inform endpoint that traffic was forwarded on behalf of this load balancer.
	r.Header.Set("X-Forwarded-Host", r.Header.Get("Host"))

	// Overwrite request's Host Field
	r.Host = addr.Host

	revProx := httputil.NewSingleHostReverseProxy(addr)
	revProx.ErrorHandler = func (w http.ResponseWriter, r *http.Request, err error) {
		log.Println(err)
		rr.SetDead(CurIndex)
		reverseProxy(w,r)
	}
	revProx.ServeHTTP(w, r)
}


func main () {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()

	lb_port := os.Getenv("LB_PORT")
	lb_domain := os.Getenv("LB_DOMAIN")
	server := http.Server{
		Addr: lb_domain + ":" + lb_port,
		ReadTimeout: 3 * time.Second,
		WriteTimeout: 3 * time.Second,
		IdleTimeout: 10 * time.Second,
	}
	
	http.HandleFunc("/", reverseProxy)
	
	go func () {
		log.Fatal(server.ListenAndServe())
	}()
	
	fmt.Println("Load Balancer is listening on:", lb_domain + ":" + lb_port)

	<- ctx.Done()
	fmt.Println("Gracefully shutting down...")
	time.Sleep(3 * time.Second)
}