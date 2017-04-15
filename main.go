package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
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

type Configuration struct {
	DBHost   string
	DBUser   string
	DBPasswd string
	MQTTHost string
	Group    string
	Sensor   []ConfigurationSensor
}

type ConfigurationSensor struct {
	DomoticzName string
	VaderName    string
}

var blowup bool
var premult bool
var temperature string
var tempKitchen string
var configFile = "config.yaml"

func main() {
	c2 := Configuration{
		DBHost: "test",
		DBUser: "test2",
	}

	out, _ := yaml.Marshal(c2)
	fmt.Printf("YAML ut: %s", string(out))

	logger, level, err := createLogger()

	if err != nil {
		fmt.Printf("Could not initialize zap logger: %v\n", err)
		os.Exit(1)
	}

	level.SetLevel(zapcore.DebugLevel)

	logger.Info("Starting up...\n")
	config := Configuration{}

	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		logger.Fatal("Could not read config file", zap.String("config file", configFile))
	}

	logger.Debug("Config file data", zap.String("config file data", string(data)))

	err = yaml.Unmarshal(data, &config)
	if err != nil {
		logger.Fatal("error", zap.Error(err))
	}
	logger.Debug("Read config file", zap.String("DBHost", config.DBHost))

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

func newDevelopmentConfig() zap.Config {
	dyn := zap.NewAtomicLevel()
	dyn.SetLevel(zap.DebugLevel)

	return zap.Config{
		Level:            dyn,
		Development:      true,
		Encoding:         "console",
		EncoderConfig:    newEncoderConfig(),
		OutputPaths:      []string{"stderr"},
		ErrorOutputPaths: []string{"stderr"},
	}
}

func createLogger() (*zap.Logger, zap.AtomicLevel, error) {
	cfg := newDevelopmentConfig()
	logger, err := cfg.Build()
	if err != nil {
		return nil, zap.AtomicLevel{}, err
	}
	return logger, cfg.Level, nil
}
