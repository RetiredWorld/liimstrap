package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"
)

type Config struct {
	Machines []struct {
		Name string `json:"name"`
		Mac  string `json:"mac"`
	} `json:"machines"`
}

type ClientInfo struct {
	Name    string
	Version string
	Mac     string
	IP      string
	Time    time.Time
	Uptime  time.Duration
}

const ALIVE_TIMEOUT = 120 * time.Second

func (ci *ClientInfo) Status() string {
	if time.Since(ci.Time) > ALIVE_TIMEOUT {
		return "down"
	}
	return "ok"
}

func (ci *ClientInfo) TimeStr() string {
	if ci.Time.IsZero() {
		return "Never"
	}
	return ci.Time.Format("2006-01-02 15:04:05")
}

// Modified from https://gist.github.com/harshavardhana/327e0577c4fed9211f65
func (ci *ClientInfo) UptimeStr() string {
	d := ci.Uptime
	if d == 0 {
		return ""
	}
	days := int64(d.Hours() / 24)
	hours := int64(math.Mod(d.Hours(), 24))
	minutes := int64(math.Mod(d.Minutes(), 60))
	seconds := int64(math.Mod(d.Seconds(), 60))
	if days < 1 {
		return fmt.Sprintf("%d:%02d:%02d",
			hours, minutes, seconds)
	}
	daysPlural := "s"
	if days == 1 {
		daysPlural = ""
	}
	return fmt.Sprintf("%d day%s, %d:%02d:%02d",
		days, daysPlural, hours, minutes, seconds)
}

var (
	config       Config
	configFile   string
	listenPort   int
	dumpTemplate bool

	macList    []string
	clientData = make(map[string]*ClientInfo)

	//go:embed index.html
	indexTemplateStr string
	indexTemplate    *template.Template
)

func loadConfig() error {
	s, err := ioutil.ReadFile(configFile)
	if err != nil {
		return err
	}
	err = json.Unmarshal(s, &config)
	if err != nil {
		return err
	}

	macList = make([]string, len(config.Machines))
	for i, m := range config.Machines {
		macList[i] = m.Mac
		clientData[m.Mac] = &ClientInfo{Name: m.Name}
	}
	if _, ok := clientData[""]; !ok {
		clientData[""] = &ClientInfo{Name: "Unknown"}
	}
	return nil
}

func handleFunc(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		// Render HTML list
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)

		// Construct data
		payload := make([]ClientInfo, len(clientData))
		for i, mac := range macList {
			payload[i] = *clientData[mac]

			var s string
			for j := 0; j < len(mac); j += 2 {
				s += ":" + mac[j:j+2]
			}
			if len(s) > 0 {
				s = s[1:]
			}
			payload[i].Mac = s
		}

		err := indexTemplate.Execute(w, payload)
		if err != nil {
			log.Printf("Error rendering index template: %v", err)
		}
	} else if r.Method == "POST" {
		r.ParseForm()
		mac := r.PostFormValue("mac")
		version := r.PostFormValue("version")
		uptimeStr := r.PostFormValue("uptime")
		if mac == "" || version == "" || uptimeStr == "" {
			http.Error(w, "OK", http.StatusBadRequest)
			return
		}
		uptime, err := strconv.Atoi(uptimeStr)
		if err != nil {
			log.Printf("Invalid uptime %#v: %v", uptimeStr, err)
			http.Error(w, "OK", http.StatusBadRequest)
			return
		}

		d, ok := clientData[mac]
		if !ok {
			d, ok = clientData[""]
			if !ok {
				http.Error(w, "OK", http.StatusOK)
				return
			}
		}

		ip := r.RemoteAddr[:strings.LastIndex(r.RemoteAddr, ":")]
		if ip[0] == '[' {
			ip = ip[1 : len(ip)-1]
		}
		d.IP = ip
		d.Time = time.Now()
		d.Version = version
		d.Uptime = time.Duration(uptime) * time.Second
		http.Error(w, "OK", http.StatusOK)
	} else {
		http.Error(w, "OK", http.StatusMethodNotAllowed)
	}
}

func init() {
	flag.StringVar(&configFile, "c", "clients.json", "JSON config of clients")
	flag.IntVar(&listenPort, "p", 3000, "Port to listen on")
	flag.BoolVar(&dumpTemplate, "t", false, "dump template")

	indexTemplate = template.Must(template.New("index").Parse(indexTemplateStr))
}

func main() {
	flag.Parse()
	if dumpTemplate {
		os.Stdout.Write([]byte(indexTemplateStr))
		return
	}

	// $INVOCATION_ID is set by systemd v232+
	if _, ok := os.LookupEnv("INVOCATION_ID"); ok {
		log.SetFlags(log.Flags() &^ (log.Ldate | log.Ltime))
	}

	err := loadConfig()
	if err != nil {
		log.Fatalf("Cannot load config: %v", err)
	}

	http.HandleFunc("/", handleFunc)
	log.Fatal(http.ListenAndServe(fmt.Sprintf("0.0.0.0:%d", listenPort), nil))
}