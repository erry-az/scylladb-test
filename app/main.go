package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/yugabyte/gocql"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Type string

const (
	Redis  Type = "redis"
	Scylla Type = "scylla"

	KeyTest = "test-"
)

var (
	log           *zap.Logger
	scyllaSession *gocql.Session
	rdb           *redis.Client
	resultSet     map[Type][]time.Duration
	resultGet     map[Type][]time.Duration
)

func main() {
	log = CreateLogger("debug")

	rdb = redis.NewClient(&redis.Options{
		Addr: "172.10.0.3:6379",
	})
	ping, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	log.Info(ping)

	scyllaSession, err = ConnectScylla("tracking", "scylla-node1", "scylla-node2", "scylla-node3")
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
	defer scyllaSession.Close()

	http.HandleFunc("/run", run)
	http.HandleFunc("/result", result)

	log.Info("connect on port : 8090")
	err = http.ListenAndServe(":8090", nil)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
}

func run(w http.ResponseWriter, r *http.Request) {
	var (
		num int64
		err error
	)

	qNum := r.URL.Query().Get("num")

	if qNum != "" {
		num, err = strconv.ParseInt(qNum, 10, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("bad request num is string"))
			return
		}
	} else {
		num = 10
	}

	if num < 1 {
		num = 10
	}

	if num > 100 {
		num = 100
	}

	resultGet = make(map[Type][]time.Duration)
	resultSet = make(map[Type][]time.Duration)

	// running redis
	go func() {
		ctx := context.Background()
		// Set
		execSet(Redis, num, func(i int, val string) {
			res, err := rdb.Set(ctx, val, i, 500*time.Second).Result()
			if err != nil {
				log.Error(err.Error())
			}

			log.Info(res)
		})
	}()

	// running scyllaDB
	go func() {
		// Set
		execSet(Scylla, num, func(i int, val string) {
			err := scyllaSession.
				Query(`INSERT INTO heart_rate_ttl (id, name, value) VALUES (?, ?, ?) USING TTL 500`,
					gocql.TimeUUID(), val, i).
				Exec()
			if err != nil {
				log.Error(err.Error())
			}
		})
	}()
}

func execSet(p Type, len int64, run func(i int, val string)) {
	resultSet[p] = make([]time.Duration, len)
	for i := range resultSet[p] {
		startTime := time.Now()
		run(i, KeyTest+strconv.Itoa(i))
		resultSet[p][i] = time.Now().Sub(startTime)
	}
}

func execGet(p Type, len int64, run func(i int, val string)) {
	resultGet[p] = make([]time.Duration, len)
	for i := range resultGet[p] {
		startTime := time.Now()
		run(i, KeyTest+strconv.Itoa(i))
		resultGet[p][i] = time.Now().Sub(startTime)
	}
}

func result(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("%+v", resultSet)))
}

func ConnectScylla(keyspace string, hosts ...string) (*gocql.Session, error) {
	cluster := gocql.NewCluster(hosts...)
	if keyspace != "" {
		cluster.Keyspace = keyspace
	}
	return cluster.CreateSession()
}

func CreateLogger(level string) *zap.Logger {
	lvl := zap.NewAtomicLevel()
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl.SetLevel(zap.InfoLevel)
	}
	encoderCfg := zap.NewDevelopmentEncoderConfig()
	logger := zap.New(zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderCfg),
		zapcore.Lock(os.Stdout),
		lvl,
	))
	return logger
}
