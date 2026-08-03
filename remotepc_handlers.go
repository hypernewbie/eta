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
	Destination string `json:"destination"`
	Phase       string `json:"phase"`
	URL         string `json:"url,omitempty"`
	// Adopted means eta was already running on that PC and this session
	// simply connected to it, rather than installing and starting one.
	// Worth telling the user: it explains why setup was instant, and it
	// means disconnecting leaves their eta running.
	Adopted bool     `json:"adopted,omitempty"`
	Error   string   `json:"error,omitempty"`
	Recent  []string `json:"recent,omitempty"`
}

func (s *server) handleRemotePCConnect(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Destination string `json:"destination"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	destination := strings.TrimSpace(request.Destination)
	if destination == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no SSH destination given"})
		return
	}
	if err := s.remotePCs.ConnectAsync(remotepc.Options{
		Destination: destination,
		// The address the browser used to reach this server is also
		// the address the ssh forward can be reached on. Empty Host
		// would fall back to 127.0.0.1, which only works for a
		// browser on the same machine.
		Host: r.Host,
		// Forward the coordinator's access verifier so the remote
		// installs with the same password the user already has. Empty
		// when the coordinator itself has no password configured.
		AccessHash: s.access.EncodedVerifier(),
	}); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, remotePCStatus{Destination: destination, Phase: string(remotepc.PhaseConnecting)})
}

func (s *server) handleRemotePCStatus(w http.ResponseWriter, r *http.Request) {
	destination := strings.TrimSpace(r.URL.Query().Get("destination"))
	if destination == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no SSH destination given"})
		return
	}
	if session, ok := s.remotePCs.Pending(destination); ok {
		status := remotePCStatus{
			Destination: destination,
			Phase:       string(session.Phase()),
			Adopted:     session.Adopted(),
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
				peer := peers.Peer{
					SSHDestination: destination,
					URL:            session.URL(),
				}
				// The browser renders each peer with peer.name.toUpperCase()
				// and the destination-stamped accent and glyph (see
				// refreshEtaMenu and desktopIconModel in web/app.ts), so a
				// record without identity fields is a guaranteed crash.
				// handlePeerAdd probes the peer's /api/identity for the same
				// reason; an SSH setup must do the same.
				//
				// Preserve the identity on a reconnect, since the URL alone
				// changes every session and a re-probe would race the user's
				// reconnect click. A first-time setup against this destination
				// has no existing record, so probe and fill the fields.
				if existing, found, err := s.peers.FindBySSHDestination(destination); err == nil && found {
					peer.Name = existing.Name
					peer.ID = existing.ID
					peer.Accent = existing.Accent
					peer.Glyph = existing.Glyph
				} else if identity, err := probePeer(r.Context(), session.URL()); err == nil {
					peer.Name = identity.Hostname
					peer.ID = identity.ID
					peer.Accent = identity.Accent
					peer.Glyph = identity.Glyph
				}
				if err := s.peers.UpsertBySSHDestination(peer); err != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
					return
				}
			}
		}
		if err := session.Err(); err != nil {
			status.Error = err.Error()
		}
		writeJSON(w, http.StatusOK, status)
		return
	}
	// No session yet. A connect attempt that is still running — the
	// adopt-probe and detectShell stages live here, typically 2-30 s —
	// reports "connecting" rather than "disconnected", so the very
	// first poll after the POST does not stop polling on a would-be
	// successful setup.
	if s.remotePCs.Starting(destination) {
		writeJSON(w, http.StatusOK, remotePCStatus{
			Destination: destination,
			Phase:       string(remotepc.PhaseConnecting),
		})
		return
	}
	// A Begin-time error (dev-build refusal, detectShell, freeLocalPort,
	// missing ssh, pipe/Start) is the only state that has nothing on
	// disk, so it is memoized. The browser renders the reason instead
	// of guessing at "disconnected".
	if err, failed := s.remotePCs.LastFailure(destination); failed {
		writeJSON(w, http.StatusOK, remotePCStatus{
			Destination: destination,
			Phase:       string(remotepc.PhaseFailed),
			Error:       err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, remotePCStatus{Destination: destination, Phase: "disconnected"})
}

func (s *server) handleRemotePCDisconnect(w http.ResponseWriter, r *http.Request) {
	destination := strings.TrimSpace(r.URL.Query().Get("destination"))
	if destination == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no SSH destination given"})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	destination := strings.TrimSpace(request.Destination)
	if destination == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no SSH destination given"})
		return
	}
	// Refused for an eta that was already running there: this computer
	// did not install it, may not know where it came from, and removing
	// ~/.eta would either do nothing or delete something it does not own.
	// Checked before disconnecting, so a refusal does not also drop a
	// working connection.
	if session, live := s.remotePCs.Get(destination); live && session.Adopted() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": remotepc.ErrAdopted.Error()})
		return
	}
	// Stop first: removing the binary out from under a running process
	// leaves it running from a deleted file.
	s.remotePCs.Disconnect(destination)
	if err := remotepc.Cleanup(r.Context(), destination); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if s.peers != nil {
		if peer, found, err := s.peers.FindBySSHDestination(destination); err == nil && found {
			_ = s.peers.Remove(peer.URL)
		}
	}
	writeJSON(w, http.StatusOK, remotePCStatus{Destination: destination, Phase: "removed"})
}
