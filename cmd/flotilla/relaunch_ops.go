package main

import (
	"time"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/launch"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/workspace"
)

type resumeOpsLeaves struct {
	resolve               func(string) (string, deliver.ResolveOutcome, error)
	assess                func(string) surface.State
	respawn               func(string, string, string) error
	readMarker            func(string) (string, error)
	killPane              func(string) error
	hasSession            func(string) (bool, error)
	newSession, newWindow func(string, string, string, string) (string, error)
	tag                   func(string, string) error
	preLaunch             func()
}

func newResumeOps(l resumeOpsLeaves, chain launch.Recipe, defaultSurface string, paneCommand func(string) (string, error)) resumeOps {
	return resumeOps{resolve: l.resolve, assess: l.assess, respawn: l.respawn, readMarker: l.readMarker, killPane: l.killPane, hasSession: l.hasSession, newSession: l.newSession, newWindow: l.newWindow, tag: l.tag, preLaunch: l.preLaunch,
		reconcile: func(agent, target, slot, _ string) error {
			return reconcileRelaunchOverlay(agent, target, slot, chain, defaultSurface, workspace.ActiveOverlay{}, paneCommand)
		}}
}

type recycleOpsLeaves struct {
	resolve        func(string) (string, deliver.ResolveOutcome, error)
	paneID         func(string) (string, error)
	inMode         func(string) (bool, error)
	assess         func(string) surface.State
	composer       func(string) surface.ComposerDisposition
	absent         func(string, string) (bool, error)
	durable        func(string, string, int) (bool, error)
	deliver        func(string, string) error
	closeFn        func(string) error
	remainOnExit   func(string, bool) error
	paneDead       func(string) (bool, error)
	selfHeal       func(string)
	respawn        func(string, string, string) error
	readMarker     func(string) (string, error)
	stampGen       func(string, string) error
	readGen        func(string) (string, error)
	lock           func(string) (func(), error)
	sleep          func(time.Duration)
	rotate         func(string) error
	cwd            string
	removeWorktree bool
	capturePane    func(string) (string, error)
	answerMenu     func(string, string) error
	countDirty     func(string) (int, error)
}

func newRecycleOps(l recycleOpsLeaves, chain launch.Recipe, defaultSurface string, paneCommand func(string) (string, error)) recycleOps {
	return recycleOps{resolve: l.resolve, paneID: l.paneID, inMode: l.inMode, assess: l.assess, composer: l.composer, absent: l.absent, durable: l.durable, deliver: l.deliver, closeFn: l.closeFn, remainOnExit: l.remainOnExit, paneDead: l.paneDead, selfHeal: l.selfHeal, respawn: l.respawn, readMarker: l.readMarker, stampGen: l.stampGen, readGen: l.readGen, lock: l.lock, sleep: l.sleep, rotate: l.rotate, cwd: l.cwd, removeWorktree: l.removeWorktree, capturePane: l.capturePane, answerMenu: l.answerMenu, countDirty: l.countDirty,
		reconcile: func(agent, target, slot, _ string) error {
			return reconcileRelaunchOverlay(agent, target, slot, chain, defaultSurface, workspace.ActiveOverlay{}, paneCommand)
		}}
}

type switchOpsLeaves struct {
	resolve      func(string) (string, deliver.ResolveOutcome, error)
	paneID       func(string) (string, error)
	inMode       func(string) (bool, error)
	assess       func(string) surface.State
	composer     func(string) surface.ComposerDisposition
	absent       func(string, string) (bool, error)
	durable      func(string, string, int) (bool, error)
	deliver      func(string, string) error
	closeFn      func(string) error
	remainOnExit func(string, bool) error
	paneDead     func(string) (bool, error)
	selfHeal     func(string)
	respawn      func(string, string, string) error
	readMarker   func(string) (string, error)
	stampGen     func(string, string) error
	readGen      func(string) (string, error)
	lock         func(string) (func(), error)
	recordPhase  func(string) error
	writeBundle  func() error
	sleep        func(time.Duration)
}

func newSwitchOps(l switchOpsLeaves, agent, selectedSlot string, chain launch.Recipe, defaultSurface string, metadata workspace.ActiveOverlay, paneCommand func(string) (string, error)) switchOps {
	return switchOps{resolve: l.resolve, paneID: l.paneID, inMode: l.inMode, assess: l.assess, composer: l.composer, absent: l.absent, durable: l.durable, deliver: l.deliver, closeFn: l.closeFn, remainOnExit: l.remainOnExit, paneDead: l.paneDead, selfHeal: l.selfHeal, respawn: l.respawn, readMarker: l.readMarker, stampGen: l.stampGen, readGen: l.readGen, lock: l.lock, recordPhase: l.recordPhase, writeBundle: l.writeBundle, sleep: l.sleep,
		writeOverlay: func(target string) error {
			return reconcileRelaunchOverlay(agent, target, selectedSlot, chain, defaultSurface, metadata, paneCommand)
		}}
}
