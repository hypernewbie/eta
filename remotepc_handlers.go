package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/hypernewbie/eta/internal/peers"
	"github.com/hypernewbie/eta/internal/remotepc"
)

// Connecting is asynchronous: a first connect to a PC compiles eta from
// source there and can take minutes. So POST starts it and returns at
// once, and the browser polls GET for the phase. Holding a request open
// for the whole thing would tie the outcome to one fetch surviving.

type remotePCStatus struct {
	Destination string   `json:"destination"`
	Phase       string   `json:"phase"`
	URL         string   `json:"url,omitempty"`
	Error       string   `json:"error,omitempty"`
	Recent      []string `json:"recent,omitempty"`
}

func (s *server) handleRemotePCConnect(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Destination string `json:"destination"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	destination := strings.TrimSpace(request.Destination)
	if destination == "" {
		http.Error(w, "no SSH destination given", http.StatusBadRequest)
		return
	}
	if err := s.remotePCs.ConnectAsync(remotepc.Options{Destination: destination}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusAccepted, remotePCStatus{Destination: destination, Phase: string(remotepc.PhaseConnecting)})
}

func (s *server) handleRemotePCStatus(w http.ResponseWriter, r *http.Request) {
	destination := strings.TrimSpace(r.URL.Query().Get("destination"))
	if destination == "" {
		http.Error(w, "no SSH destination given", http.StatusBadRequest)
		return
	}
	session, ok := s.remotePCs.Pending(destination)
	if !ok {
		writeJSON(w, http.StatusOK, remotePCStatus{Destination: destination, Phase: "disconnected"})
		return
	}
	status := remotePCStatus{
		Destination: destination,
		Phase:       string(session.Phase()),
		Recent:      session.Recent(),
	}
	if session.Phase() == remotepc.PhaseReady {
		status.URL = session.URL()
		// Recorded only once it actually works, and keyed by destination
		// rather than URL: the forwarded port changes every reconnect, so
		// keying on the URL would strand the previous entry pointing at a
		// closed port. Guarded because an instance can run without a peer
		// inventory, as every other peer-touching handler here allows.
		if s.peers != nil {
			if err := s.peers.UpsertBySSHDestination(peers.Peer{
				SSHDestination: destination,
				URL:            session.URL(),
			}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}
	if err := session.Err(); err != nil {
		status.Error = err.Error()
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *server) handleRemotePCDisconnect(w http.ResponseWriter, r *http.Request) {
	destination := strings.TrimSpace(r.URL.Query().Get("destination"))
	if destination == "" {
		http.Error(w, "no SSH destination given", http.StatusBadRequest)
		return
	}
	// Stops the remote eta too, since its stdin closes with the
	// connection. The peer entry stays: it is the durable record that
	// this PC is known, and reconnecting is how it comes back.
	s.remotePCs.Disconnect(destination)
	writeJSON(w, http.StatusOK, remotePCStatus{Destination: destination, Phase: "disconnected"})
}

// handleRemotePCCleanup removes eta's files from the PC and forgets it.
// Separate from disconnecting, which is routine.
func (s *server) handleRemotePCCleanup(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Destination string `json:"destination"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	destination := strings.TrimSpace(request.Destination)
	if destination == "" {
		http.Error(w, "no SSH destination given", http.StatusBadRequest)
		return
	}
	// Stop first: removing the binary out from under a running process
	// leaves it running from a deleted file.
	s.remotePCs.Disconnect(destination)
	if err := remotepc.Cleanup(r.Context(), destination); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if s.peers != nil {
		if peer, found, err := s.peers.FindBySSHDestination(destination); err == nil && found {
			_ = s.peers.Remove(peer.URL)
		}
	}
	writeJSON(w, http.StatusOK, remotePCStatus{Destination: destination, Phase: "removed"})
}
