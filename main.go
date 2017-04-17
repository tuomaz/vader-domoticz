package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"syscall"
	"time"

	"strconv"

	"github.com/go-resty/resty"
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

// sample = {'name' : row[1], 'value' : float(row[0]), 'type' : 'TEMPERATURE', 'timestamp' : row[2] * 1000}
type VaderData struct {
	Name       string  `json:"name"`
	Value      float64 `json:"value"`
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
	Domoticz string
	Vader    string
	SType    string
}

var blowup bool
var premult bool
var temperature string
var tempKitchen string
var configFile = "config.yaml"
var config Configuration
var logger *zap.Logger

func init() {
	flag.StringVar(&configFile, "configfile", "config.yaml", "The config file to use, default config.yaml")
}

func main() {
	flag.Parse()
	/*
		c2 := Configuration{}
		c2.DBHost = "TEST"
		c2.Sensor = make([]ConfigurationSensor, 0)

		c2.Sensor = append(c2.Sensor, ConfigurationSensor{Domoticz: "A", Vader: "B"})
		c2.Sensor = append(c2.Sensor, ConfigurationSensor{Domoticz: "C", Vader: "D"})

		b, _ := yaml.Marshal(c2)
		fmt.Printf("%s", string(b))

		os.Exit(0)
	*/
	logger, level, err := createLogger()

	if err != nil {
		fmt.Printf("Could not initialize zap logger: %v\n", err)
		os.Exit(1)
	}

	level.SetLevel(zapcore.DebugLevel)

	logger.Info("Starting up...\n")
	config = Configuration{}

	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		logger.Fatal("Could not read config file", zap.String("config file", configFile))
	}

	err = yaml.Unmarshal(data, &config)
	if err != nil {
		logger.Fatal("error", zap.Error(err))
	}

	logger.Debug("Lengt of sensors list", zap.Int("len", len(config.Sensor)))

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
		Address:  fmt.Sprintf("%s:1883", config.MQTTHost),
		ClientID: []byte("vader"),
	})
	if err != nil {
		panic(err)
	}

	logger.Debug("Connected to server")

	// Subscribe to topics.
	err = cli.Subscribe(&client.SubscribeOptions{
		SubReqs: []*client.SubReq{
			&client.SubReq{
				TopicFilter: []byte("domoticz/out"),
				QoS:         mqtt.QoS0,
				// Define the processing of the message handler.
				Handler: func(topicName, message []byte) {
					dd := &DomoticzsData{}
					err := json.Unmarshal(message, dd)
					if err != nil {
						logger.Error("Failed to unmarshall data", zap.Error(err))
					}
					name, stype := findVaderTypes(dd.Name)
					valueFloat, err := strconv.ParseFloat(dd.Svalue1, 64)
					if err != nil {
						logger.Error("Could not parse float", zap.String("value", dd.Svalue1))
						return
					}
					if name != "" {
						vd := VaderData{
							Name:       name,
							Value:      valueFloat,
							SensorType: stype,
							Timestamp:  fmt.Sprintf("%d", time.Now().Unix()*1000),
						}
						sendData(vd)
					}
					logger.Debug("Data", zap.String("name", name), zap.String("value", dd.Svalue1))

				},
			},
		},
	})
	if err != nil {
		panic(err)
	}

	logger.Debug("Awaiting signal")
	<-done
	logger.Info("Exiting")

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

func findVaderTypes(domoticz string) (string, string) {
	for _, s := range config.Sensor {
		if s.Domoticz == domoticz {
			return s.Vader, s.SType
		}
	}
	return "", ""
}

func sendData(vd VaderData) {
	list := make([]VaderData, 0)
	list = append(list, vd)
	resp, err := resty.R().
		SetBody(list).
		Post("http://t.jpl.se/vader/rest/save")
	if err != nil {
		logger.Error("Failed to post vader data", zap.Error(err))
		return
	}
	if resp == nil {
		logger.Debug("Respsonse is nil")
	} else {
		fmt.Printf("status code is %d", resp.StatusCode())
		resp.Error()
	}
}
