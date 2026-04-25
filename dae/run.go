/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2023, daeuniverse Organization <team@v2raya.org>
 */

package dae

import (
	"fmt"
	"net/netip"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/daeuniverse/dae/common/netutils"
	daeConfig "github.com/daeuniverse/dae/config"
	"github.com/daeuniverse/dae/control"
	"github.com/daeuniverse/dae/pkg/config_parser"
	"github.com/daeuniverse/dae/pkg/logger"
	"github.com/daeuniverse/outbound/protocol/direct"
	"github.com/mohae/deepcopy"
	"github.com/sirupsen/logrus"
)

var ErrControlPlaneNotInit = fmt.Errorf("control plane doesn't init yet")

type ReloadMessage struct {
	Config   *daeConfig.Config
	Callback chan<- error
}

type serveResult struct {
	listener *control.Listener
	err      error
}

var ChReloadConfigs = make(chan *ReloadMessage)
var GracefullyExit = make(chan struct{})
var EmptyConfig *daeConfig.Config
var onceWaitingNetwork sync.Once
var controlPlaneRef atomic.Pointer[control.ControlPlane]

func init() {
	sections, err := config_parser.Parse(`global{} routing{}`)
	if err != nil {
		panic(err)
	}
	EmptyConfig, err = daeConfig.New(sections)
	if err != nil {
		panic(err)
	}
}

func ControlPlane() (*control.ControlPlane, error) {
	if c := controlPlaneRef.Load(); c != nil {
		return c, nil
	}
	return nil, ErrControlPlaneNotInit
}

func storeControlPlane(c *control.ControlPlane) {
	controlPlaneRef.Store(c)
}

func reconfigureLoggers(log *logrus.Logger, logLevel string, disableTimestamp bool) {
	logger.SetLogger(log, logLevel, disableTimestamp, nil)
	std := logrus.StandardLogger()
	if log != std {
		logger.SetLogger(std, logLevel, disableTimestamp, nil)
	}
}

func notifyReloadCallback(callback chan<- error, err error) {
	if callback == nil {
		return
	}
	callback <- err
}

func Run(log *logrus.Logger, conf *daeConfig.Config, externGeoDataDirs []string, disableTimestamp bool, dry bool) (err error) {
	defer close(GracefullyExit)
	// Not really run dae.
	if dry {
		storeControlPlane(nil)
		log.Infoln("Dry run in api-only mode")
	dryLoop:
		for newConf := range ChReloadConfigs {
			switch newConf {
			case nil:
				break dryLoop
			default:
				notifyReloadCallback(newConf.Callback, nil)
			}
		}
		return nil
	}

	// New c.
	c, err := newControlPlane(log, nil, nil, conf, externGeoDataDirs)
	if err != nil {
		return err
	}
	storeControlPlane(c)
	defer storeControlPlane(nil)

	// Serve tproxy TCP/UDP server util signals.
	serveDoneCh := make(chan serveResult, 1)
	go func() {
		readyChan := make(chan bool, 1)
		go func() {
			<-readyChan
			log.Infoln("Ready")
		}()
		var listener *control.Listener
		var serveErr error
		control.GetDaeNetns().With(func() error {
			if listener, serveErr = c.ListenAndServe(readyChan, conf.Global.TproxyPort); serveErr != nil {
				log.Errorln("ListenAndServe:", serveErr)
			}
			return serveErr
		})
		serveDoneCh <- serveResult{listener: listener, err: serveErr}
	}()
	reloading := false
	/* dae-wing start */
	var errReload error
	var chCallback chan<- error
	var pendingControlPlane *control.ControlPlane
	var pendingConf *daeConfig.Config
	var pendingCallback chan<- error
	/* dae-wing end */
loop:
	for {
		select {
		case result := <-serveDoneCh:
			if reloading {
				if result.listener == nil {
					// Failed to listen. Exit.
					break loop
				}
				// Serve.
				c = pendingControlPlane
				conf = pendingConf
				chCallback = pendingCallback
				pendingControlPlane = nil
				pendingConf = nil
				pendingCallback = nil
				storeControlPlane(c)
				reloading = false
				log.Warnln("[Reload] Serve")
				readyChan := make(chan bool, 1)
				go func(listener *control.Listener) {
					if err := c.Serve(readyChan, listener); err != nil {
						log.Errorln("ListenAndServe:", err)
						serveDoneCh <- serveResult{listener: listener, err: err}
						return
					}
					serveDoneCh <- serveResult{listener: listener, err: nil}
				}(result.listener)
				<-readyChan
				log.Warnln("[Reload] Finished")
				/* dae-wing start */
				notifyReloadCallback(chCallback, errReload)
				/* dae-wing end */
			} else {
				// Listening error.
				break loop
			}
		case newReloadMsg := <-ChReloadConfigs:
			// Reload signal.
			log.Warnln("[Reload] Received reload signal; prepare to reload")

			/* dae-wing start */
			newConf := newReloadMsg.Config
			/* dae-wing end */
			// Reconfigure logger in place to preserve writer/locks and avoid
			// swapping the logger object out from under concurrent users.
			reconfigureLoggers(log, newConf.Global.LogLevel, disableTimestamp)

			// New control plane.
			obj := c.EjectBpf()
			// Do not clone dns cache on reload.
			// The current dae-core reload path no longer restores the cloned cache
			// into the new controller/domain-routing state, so copying it here only
			// adds reload-time allocations without preserving useful runtime state.
			var dnsCache map[string]*control.DnsCache
			log.Warnln("[Reload] Load new control plane")
			newC, err := newControlPlane(log, obj, dnsCache, newConf, externGeoDataDirs)
			if err != nil {
				/* dae-wing start */
				errReload = err
				/* dae-wing end */

				log.WithFields(logrus.Fields{
					"err": err,
				}).Errorln("[Reload] Failed to reload; try to roll back configuration")
				// Load last config back.
				newC, err = newControlPlane(log, obj, dnsCache, conf, externGeoDataDirs)
				if err != nil {
					obj.Close()
					c.Close()
					log.WithFields(logrus.Fields{
						"err": err,
					}).Fatalln("[Reload] Failed to roll back configuration")
				}
				newConf = conf
				log.Errorln("[Reload] Last reload failed; rolled back configuration")
			} else {
				log.Warnln("[Reload] Stopped old control plane")

				/* dae-wing start */
				errReload = nil
				/* dae-wing end */
			}

			// Inject bpf objects into the new control plane life-cycle.
			newC.InjectBpf(obj)

			// Prepare new context.
			oldC := c
			pendingControlPlane = newC
			pendingConf = newConf
			reloading = true
			/* dae-wing start */
			pendingCallback = newReloadMsg.Callback
			/* dae-wing end */

			// Ready to close.
			oldC.Close()
		}
	}
	storeControlPlane(nil)
	if e := c.Close(); e != nil {
		return fmt.Errorf("close control plane: %w", e)
	}
	return nil
}

func newControlPlane(log *logrus.Logger, bpf interface{}, dnsCache map[string]*control.DnsCache, conf *daeConfig.Config, externGeoDataDirs []string) (c *control.ControlPlane, err error) {

	// Print configuration.
	if log.IsLevelEnabled(logrus.DebugLevel) {
		bConf, _ := conf.Marshal(2)
		log.Debugln(string(bConf))
	}

	// Deep copy to prevent modification.
	conf = deepcopy.Copy(conf).(*daeConfig.Config)

	// Init Direct Dialers.
	direct.InitDirectDialers(conf.Global.FallbackResolver)
	netutils.FallbackDns = netip.MustParseAddrPort(conf.Global.FallbackResolver)

	if !conf.Global.DisableWaitingNetwork && len(conf.Global.WanInterface) > 0 {
		// Wait for network for WAN ready.
		onceWaitingNetwork.Do(func() {
			WaitForNetwork(log)
		})
	}

	/// Get subscription -> nodeList mapping.
	subscriptionToNodeList := map[string][]string{}
	if len(conf.Node) > 0 {
		for _, node := range conf.Node {
			subscriptionToNodeList[""] = append(subscriptionToNodeList[""], string(node))
		}
	}
	if len(conf.Subscription) > 0 {
		return nil, fmt.Errorf("daeConfig.subscription is not supported")
	}

	if err = preprocessWanInterfaceAuto(conf); err != nil {
		return nil, err
	}

	// New dae control plane.
	c, err = control.NewControlPlane(
		log,
		bpf,
		dnsCache,
		subscriptionToNodeList,
		conf.Group,
		&conf.Routing,
		&conf.Global,
		&conf.Dns,
		externGeoDataDirs,
	)
	if err != nil {
		return nil, err
	}
	// Call GC to release memory.
	runtime.GC()

	return c, nil
}
