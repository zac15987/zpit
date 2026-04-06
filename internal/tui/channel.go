package tui

// channel.go — Channel events: broker EventBus subscription and event reading.
//
// Lock protocol:
//   - channelSubscribeCmd: acquires Lock to store the subscription channel in
//     AppState.channelSubs. Reads broker reference (read-only after init, no lock).
//   - channelReadNextCmd: lock-free — blocks on the subscription channel.

import (
	"errors"
	"fmt"
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zac15987/zpit/internal/broker"
)

// errChannelClosed is a sentinel error returned by channelReadNextCmd when the
// EventBus channel is closed (normal shutdown after Unsubscribe).
var errChannelClosed = errors.New("channel closed")

func (m Model) handleChannelEvent(msg ChannelEventMsg) (tea.Model, tea.Cmd) {
	m.state.AppendChannelEvent(msg.ProjectID, msg.Event)
	m.state.logger.Printf("channel: event received project=%s type=%s", msg.ProjectID, msg.Event.Type)

	// Auto-scroll: if viewing channel and at bottom, follow new content (any project).
	autoScroll := m.currentView == ViewChannel && m.viewport.AtBottom()

	// Re-issue read cmd for the next event.
	m.state.RLock()
	ch, ok := m.state.channelSubs[msg.ProjectID]
	m.state.RUnlock()
	var nextCmd tea.Cmd
	if ok && ch != nil {
		nextCmd = channelReadNextCmd(msg.ProjectID, ch, m.state.logger)
	}

	if autoScroll {
		m.viewport.GotoBottom()
	}
	return m, nextCmd
}

// channelSubscribeCmd subscribes to the broker's EventBus for the given project.
// Stores the subscription channel in AppState.channelSubs and returns a cmd that
// reads the first event. Subsequent reads are triggered by channelReadNextCmd.
func (m Model) channelSubscribeCmd(projectID string) tea.Cmd {
	logger := m.state.logger
	brokerRef := m.state.broker
	if brokerRef == nil {
		return func() tea.Msg {
			return ChannelSubscribedMsg{ProjectID: projectID, Err: fmt.Errorf("broker not available")}
		}
	}
	bus := brokerRef.Events()
	ch := bus.Subscribe(projectID)

	m.state.Lock()
	m.state.channelSubs[projectID] = ch
	m.state.Unlock()

	logger.Printf("channel: subscribed to EventBus for project=%s", projectID)
	return channelReadNextCmd(projectID, ch, logger)
}

// channelReadNextCmd returns a tea.Cmd that blocks until the next event arrives on ch.
func channelReadNextCmd(projectID string, ch <-chan broker.Event, logger *log.Logger) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			logger.Printf("channel: EventBus channel closed for project=%s", projectID)
			return ChannelSubscribedMsg{ProjectID: projectID, Err: errChannelClosed}
		}
		return ChannelEventMsg{ProjectID: projectID, Event: event}
	}
}
