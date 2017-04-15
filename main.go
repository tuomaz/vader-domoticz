package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/yosssi/gmq/mqtt"
	"github.com/yosssi/gmq/mqtt/client"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v2"
)

//DomoticzsData data from Domoticz
type DomoticzsData struct {
	ID      string `json:"id"`
	Svalue1 string `json:"svalue1"`
	Name    string `json:"name"`
}

type vaderData struct {
	Name       string  `json:"name"`
	Value      float32 `json:"value"`
	SensorType string  `json:"type"`
	Timestamp  string  `json:"timestamp"`
}

type configuration struct {
	DBHost   string
	DBPasswd string
	DBname   string
	Group    string
	Sensor   map[string]string
}

var blowup bool
var premult bool
var temperature string
var tempKitchen string
var configFile = "config.yaml"

func main() {
	logger, level, err := createLogger()(*zap.Logger, zap.AtomicLevel, error)

	if err != nil {
		fmt.Errorf("Could not initialize zap logger: %v", err)
		os.Exit(1)
	}

	level.SetLevel(zapcore.DebugLevel)

	fmt.Printf("Starting up...\n")
	config := configuration{}

	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		fmt.Printf("Could not read config file %v", configFile)
		return
	}

	err = yaml.Unmarshal([]byte(data), &config)
	if err != nil {
		log.Fatalf("error: %v", err)
	}
	fmt.Printf("--- t:\n%v\n\n", config)

	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)

	go func() {
		_ = <-sigs
		done <- true
	}()

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	cli := client.New(&client.Options{
		ErrorHandler: func(err error) {
			fmt.Println(err)
		},
	})

	// Terminate the Client.
	defer cli.Terminate()

	// Connect to the MQTT Server.
	err = cli.Connect(&client.ConnectOptions{
		Network:  "tcp",
		Address:  "localhost:1883",
		ClientID: []byte("vader"),
	})
	if err != nil {
		panic(err)
	}

	fmt.Printf("Connected to server")

	// Subscribe to topics.
	err = cli.Subscribe(&client.SubscribeOptions{
		SubReqs: []*client.SubReq{
			&client.SubReq{
				TopicFilter: []byte("domoticz/out"),
				QoS:         mqtt.QoS0,
				// Define the processing of the message handler.
				Handler: func(topicName, message []byte) {
					fmt.Printf("json = %v", string(message))
					dd := &DomoticzsData{}
					err := json.Unmarshal(message, dd)
					if err != nil {
						fmt.Printf("Failed to unmarshall data %v\n", err)
					}
					fmt.Printf("ID = %s, svalue1 = %s, name = %s\n", dd.ID, dd.Svalue1, dd.Name)
					if dd.ID == "3585" {
						temperature = dd.Svalue1
					}
					if dd.ID == "260" {
						tempKitchen = dd.Svalue1
					}
				},
			},
		},
	})
	if err != nil {
		panic(err)
	}

	fmt.Println("Awaiting signal")
	<-done
	fmt.Println("Exiting")

	// Disconnect the Network Connection.
	if err := cli.Disconnect(); err != nil {
		panic(err)
	}
}

func newEncoderConfig() zapcore.EncoderConfig {
	return zapcore.EncoderConfig{
		// Keys can be anything except the empty string.
		TimeKey:        "T",
		LevelKey:       "L",
		NameKey:        "N",
		CallerKey:      "C",
		MessageKey:     "M",
		StacktraceKey:  "S",
		EncodeLevel:    zapcore.CapitalColorLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
}

func createLogger() (*zap.Logger, zap.AtomicLevel, error) {
	cfg := newDevelopmentConfig()
	logger, err := cfg.Build()
	if err != nil {
		return nil, nil, err
	}
	return logger, cfg.Level, nil
}
