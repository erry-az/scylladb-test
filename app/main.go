package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path"
	"strconv"
	"sync"
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
	len           int64
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

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var filepath = path.Join("/src/app/views", "index.html")
		var tmpl, err = template.ParseFiles(filepath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var data = map[string]interface{}{
			"title": "Learning Golang Web",
			"name":  "Batman",
		}

		err = tmpl.Execute(w, data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
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

	len = num
	resultGet = make(map[Type][]time.Duration)
	resultSet = make(map[Type][]time.Duration)

	wg := sync.WaitGroup{}

	wg.Add(1)
	// running redis
	go func() {
		defer wg.Done()
		ctx := context.Background()
		// Set
		execSet(Redis, num, func(i int, val string) {
			_, err := rdb.Set(ctx, val, i, 500*time.Second).Result()
			if err != nil {
				log.Error(err.Error())
			}
		})

		// Get
		execGet(Redis, num, func(i int, val string) {
			res, err := rdb.Get(ctx, val).Result()
			if err != nil {
				log.Error(err.Error())
			}

			fmt.Println("redis: ", val, res)
		})
	}()

	wg.Add(1)
	// running scyllaDB
	go func() {
		defer wg.Done()
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

		// Get
		execGet(Scylla, num, func(i int, val string) {
			var (
				id    gocql.UUID
				name  string
				value int
			)
			iter := scyllaSession.
				Query(`SELECT id, name, value FROM heart_rate_ttl WHERE name=? AND value=?`,
					val, i).Iter()
			for iter.Scan(&id, &name, &value) {
				fmt.Println("scylla:", id, name, value)
			}
			if err := iter.Close(); err != nil {
				log.Error(err.Error())
			}
		})
	}()

	wg.Wait()
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Done"))
}

func execSet(p Type, len int64, run func(i int, val string)) {
	resultSet[p] = make([]time.Duration, len)
	for i := range resultSet[p] {
		num := i
		startTime := time.Now()
		run(num, KeyTest+strconv.Itoa(num))
		resultSet[p][num] = time.Now().Sub(startTime)
	}
}

func execGet(p Type, len int64, run func(i int, val string)) {
	resultGet[p] = make([]time.Duration, len)
	for i := range resultGet[p] {
		num := i
		startTime := time.Now()
		run(num, KeyTest+strconv.Itoa(num))
		resultGet[p][num] = time.Now().Sub(startTime)
	}
}

func result(w http.ResponseWriter, r *http.Request) {
	type Result struct {
		ID  int
		Get map[Type]float64
		Set map[Type]float64
	}

	results := make([]Result, len)
	for i := range results {
		num := i
		results[num] = Result{
			ID: num,
			Get: map[Type]float64{
				Scylla: float64(resultGet[Scylla][i] / time.Microsecond),
				Redis:  float64(resultGet[Redis][i] / time.Microsecond),
			},
			Set: map[Type]float64{
				Scylla: float64(resultSet[Scylla][i] / time.Microsecond),
				Redis:  float64(resultSet[Redis][i] / time.Microsecond),
			},
		}
	}

	ret, err := json.Marshal(results)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(ret)
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
