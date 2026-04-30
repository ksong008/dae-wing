package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/daeuniverse/dae-wing/cmd/internal"
	"github.com/daeuniverse/dae-wing/common"
	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/engine"
	"github.com/daeuniverse/dae-wing/orchestrator"
	"github.com/daeuniverse/dae-wing/transport/httpapi"
	"github.com/daeuniverse/dae-wing/webrender"
	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/cors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gopkg.in/natefinch/lumberjack.v2"
)

func init() {
	runCmd.PersistentFlags().StringVarP(&cfgDir, "config", "c", filepath.Join("/etc", db.AppName), "config directory")
	runCmd.PersistentFlags().StringVarP(&listen, "listen", "l", "0.0.0.0:2023", "listening address")
	runCmd.PersistentFlags().StringVar(&pprofListen, "pprof-listen", "", "optional local pprof listen address, e.g. 127.0.0.1:6061")
	runCmd.PersistentFlags().BoolVar(&apiOnly, "api-only", false, "run control-plane backend without dae")
	runCmd.PersistentFlags().StringVar(&logFile, "logfile", "", "Log file to write. Empty means writing to stdout and stderr.")
	runCmd.PersistentFlags().IntVar(&logFileMaxSize, "logfile-maxsize", 30, "Unit: MB. The maximum size in megabytes of the log file before it gets rotated.")
	runCmd.PersistentFlags().IntVar(&logFileMaxBackups, "logfile-maxbackups", 3, "The maximum number of old log files to retain.")
	runCmd.PersistentFlags().BoolVarP(&disableTimestamp, "disable-timestamp", "", false, "disable timestamp")
}

func _errorExit(err error) {
	// Notify to Close().
	logrus.Errorf("Exiting: %v", err)
	if stopErr := engine.Default().Stop(10 * time.Second); stopErr != nil {
		logrus.Errorf("Force exit after shutdown timeout: %v", stopErr)
	}
}

func errorExit(err error) {
	_errorExit(err)
	os.Exit(1)
}

var (
	cfgDir            string
	logFile           string
	logFileMaxSize    int
	logFileMaxBackups int
	disableTimestamp  bool
	listen            string
	apiOnly           bool
	pprofListen       string

	runCmd = &cobra.Command{
		Use:   "run",
		Short: "Run " + db.AppName + " in the foreground",
		Run: func(cmd *cobra.Command, args []string) {
			if cfgDir == "" {
				logrus.Fatalln("Argument \"--config\" or \"-c\" is required but not provided.")
			}
			if err := os.MkdirAll(cfgDir, 0750); err != nil && !os.IsExist(err) {
				logrus.Fatalln(err)
			}

			// Require "sudo" if necessary.
			if !apiOnly {
				internal.AutoSu()
			}

			// Read config from --config cfgDir.
			if err := db.InitDatabase(cfgDir); err != nil {
				logrus.Fatalln("Failed to init db:", err)
			}

			orchestrator.EnsureSubscriptionSchedulers(context.TODO())

			// Run dae.
			var logOpts *lumberjack.Logger
			if logFile != "" {
				logOpts = &lumberjack.Logger{
					Filename:   logFile,
					MaxSize:    logFileMaxSize,
					MaxAge:     0,
					MaxBackups: logFileMaxBackups,
					LocalTime:  true,
					Compress:   true,
				}
				logrus.SetOutput(logOpts)
				db.SetOutput(logOpts)
			}
			go func() {
				if err := engine.Default().Run(
					logrus.StandardLogger(),
					engine.Default().EmptyConfig(),
					[]string{cfgDir},
					disableTimestamp,
					apiOnly,
				); err != nil {
					logrus.Fatalln("dae.Run:", err)
				}
				os.Exit(1)
			}()
			// Reload with running state.
			if err := orchestrator.RestoreRunningState(context.TODO()); err != nil {
				logrus.Warnln("Failed to restore last running state:", err)
			}

			// ListenAndServe control-plane APIs.
			mux := http.NewServeMux()
			mux.Handle("/api/", auth(cors.AllowAll().Handler(http.StripPrefix("/api", httpapi.NewHandler()))))
			if err := webrender.Handle(mux); err != nil {
				errorExit(err)
			}
			var pprofServer *http.Server
			if pprofListen != "" {
				pprofServer = &http.Server{
					Addr:              pprofListen,
					Handler:           http.DefaultServeMux,
					ReadHeaderTimeout: 5 * time.Second,
				}
				defer func() {
					ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					defer cancel()
					_ = pprofServer.Shutdown(ctx)
				}()
				go func() {
					logrus.Printf("pprof listen on http://%s/debug/pprof/", pprofListen)
					if err := pprofServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
						logrus.Errorln("pprof ListenAndServe:", err)
					}
				}()
			}
			go func() {
				host, port, _ := net.SplitHostPort(listen)
				if host == "0.0.0.0" || host == "::" {
					addrs, err := common.GetIfAddrs()
					if err == nil {
						for _, addr := range addrs {
							addr = net.JoinHostPort(addr, port)
							logrus.Printf("Listen on http://%v", addr)
						}
						goto listenAndServe
					}
				}
				logrus.Printf("Listen on %v", listen)
			listenAndServe:
				if err := http.ListenAndServe(listen, mux); err != nil {
					errorExit(err)
				}
			}()
			sigs := make(chan os.Signal, 1)
			signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGKILL, syscall.SIGILL)
			for sig := range sigs {
				_errorExit(errors.New(sig.String()))
				return
			}
		},
	}
)

const (
	runtimeEventsAPIPath       = "/api/events/runtime"
	runtimeEventsTokenQueryKey = "access_token"
)

func requestAuthToken(r *http.Request) string {
	if authorization := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")); authorization != "" {
		return authorization
	}
	if r.Method == http.MethodGet && r.URL.Path == runtimeEventsAPIPath {
		return strings.TrimSpace(r.URL.Query().Get(runtimeEventsTokenQueryKey))
	}
	return ""
}

func auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization := requestAuthToken(r)
		var user db.User
		token, err := jwt.Parse(authorization, func(token *jwt.Token) (interface{}, error) {
			// Don't forget to validate the alg is what you expect:
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			// Get corresponding secret.
			subject, err := token.Claims.GetSubject()
			if err != nil {
				return nil, err
			}
			q := db.DB(context.TODO()).Model(&db.User{}).Where("username = ?", subject).First(&user)
			if q.Error != nil {
				return nil, q.Error
			}
			if q.RowsAffected == 0 {
				return nil, fmt.Errorf("no such user")
			}
			return []byte(user.JwtSecret), nil
		})
		ctx := r.Context()
		if err == nil {
			if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
				if expireAt, err := token.Claims.GetExpirationTime(); err == nil && time.Now().Before(expireAt.Time) {
					ctx = context.WithValue(ctx, "role", claims["role"])
					ctx = context.WithValue(ctx, "user", &user)
				}
			}
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
