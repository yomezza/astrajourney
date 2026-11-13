package planetserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/planet/access"
	"github.com/yomezza/cosmosimulator/internal/planet/movement"
	"github.com/yomezza/cosmosimulator/internal/planet/scene"
	"github.com/yomezza/cosmosimulator/internal/planet/scenestore"
	"github.com/yomezza/cosmosimulator/internal/planet/visits"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"net/http"
	"time"
)

type frame struct {
	V       int             `json:"v"`
	Type    string          `json:"type"`
	Seq     int64           `json:"seq"`
	Visit   string          `json:"visit,omitempty"`
	Epoch   int64           `json:"connection_epoch,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

func message(kind string, payload any) map[string]any {
	return map[string]any{"v": 1, "type": kind, "payload": payload}
}
func publicError(err error) string {
	switch {
	case errors.Is(err, visits.ErrLandingDistance), errors.Is(err, visits.ErrReturnOperation), errors.Is(err, visits.ErrOffer), errors.Is(err, visits.ErrOperation), errors.Is(err, access.ErrAuth), errors.Is(err, access.ErrDenied), errors.Is(err, visits.ErrUsed), errors.Is(err, visits.ErrExpired), errors.Is(err, visits.ErrReplaced), errors.Is(err, visits.ErrVisit):
		return err.Error()
	default:
		return "runtime_unavailable"
	}
}
func (a *App) websocket(w http.ResponseWriter, r *http.Request) {
	a.wsMu.Lock()
	if a.stopping.Load() || a.runContext == nil {
		a.wsMu.Unlock()
		httpkit.Fail(w, 503, "runtime_unavailable")
		return
	}
	a.wsWG.Add(1)
	a.wsMu.Unlock()
	defer a.wsWG.Done()
	if a.scenes == nil || a.stopping.Load() {
		a.health(w, 503, "runtime_unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), a.config.DBTimeout)
	id, err := access.Authenticate(ctx, a.db, r)
	if err == nil && a.coordinator.Ready(ctx) != "" {
		err = errors.New("runtime unavailable")
	}
	cancel()
	if err != nil {
		code := 503
		if errors.Is(err, access.ErrAuth) {
			code = 401
		}
		httpkit.Fail(w, code, publicError(err))
		return
	}
	a.wsMu.Lock()
	if a.connections >= 256 || a.controllers[id.User] >= 4 {
		a.wsMu.Unlock()
		httpkit.Fail(w, 429, "connection_limit")
		return
	}
	a.connections++
	a.controllers[id.User]++
	if a.entryGates[id.User] == nil {
		a.entryGates[id.User] = make(chan struct{}, 1)
	}
	gate := a.entryGates[id.User]
	a.wsMu.Unlock()
	defer func() {
		a.wsMu.Lock()
		a.connections--
		a.controllers[id.User]--
		if a.controllers[id.User] == 0 {
			delete(a.controllers, id.User)
			delete(a.entryGates, id.User)
		}
		a.wsMu.Unlock()
	}()
	// Exact Origin validation has already run in Handler; an empty browser Origin
	// is not an authorization mechanism. Cookies and a ticket/resume are required.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	// The HTTP request context may be canceled on or after a proxy's
	// WebSocket upgrade. Keep the socket alive until the peer disconnects or
	// the Planet service shuts down, rather than inheriting that cancellation.
	connection, cancelConnection := planetWebSocketContext(r.Context(), a.runContext)
	defer cancelConnection()
	r = r.WithContext(connection)
	openedAt := time.Now()
	closeCause := "handler_exit"
	var closeError error
	var epochForLog int64
	var established bool
	defer func() {
		_ = c.CloseNow()
		if a.logger == nil {
			return
		}
		if a.runContext.Err() != nil {
			closeCause = "service_shutdown"
		}
		fields := []any{
			"cause", closeCause,
			"connected", established,
			"connection_epoch", epochForLog,
			"duration_ms", time.Since(openedAt).Milliseconds(),
		}
		if closeError != nil {
			fields = append(fields, "error", closeError.Error())
		}
		if closeCause == "service_shutdown" ||
			(closeCause == "read_error" &&
				websocket.CloseStatus(closeError) == websocket.StatusNormalClosure) {
			a.logger.Debug("planet websocket ended", fields...)
		} else {
			a.logger.Warn("planet websocket ended", fields...)
		}
	}()
	c.SetReadLimit(4096)
	auth, done := context.WithTimeout(r.Context(), 5*time.Second)
	var hello frame
	err = wsjson.Read(auth, c, &hello)
	done()
	if err != nil || hello.V != 1 || hello.Type != "hello" || hello.Seq != 1 {
		closeCause = "hello_failed"
		closeError = err
		return
	}
	var input struct {
		Ticket string `json:"ticket"`
		Resume string `json:"resume_visit"`
	}
	if json.Unmarshal(hello.Payload, &input) != nil || (input.Ticket == "") == (input.Resume == "") || len(input.Ticket) > 64 {
		closeCause = "invalid_hello_payload"
		return
	}
	prepare, finish := context.WithTimeout(r.Context(), a.config.SceneLoadTimeout)
	hash := ""
	if input.Ticket != "" {
		if len(input.Ticket) != 64 {
			finish()
			return
		}
		hash = account.TokenHash(input.Ticket)
	}
	in, err := a.visits.Resolve(prepare, id, hash, input.Resume)
	var pinRelease func()
	var welcome visits.Welcome
	var pilot *movement.Member
	var localScene *scene.Scene
	var activeFence scenestore.Fence
	if err == nil {
		pin, e := a.scenes.Acquire(prepare, in.Key)
		err = e
		if err != nil && in.Visit != "" {
			if fence, fenceErr := a.sceneFence(); fenceErr == nil {
				_ = a.visits.DiagnoseSceneFailure(prepare, id, visits.Welcome{Visit: in.Visit}, fence, in.Key, err)
			}
		}
		if err == nil {
			pinRelease = pin.Release
			f, e := a.sceneFence()
			err = e
			if err == nil {
				activeFence = f
				localScene = pin.Scene()
				err = func() error {
					if err := takeGate(prepare, gate); err != nil {
						return err
					}
					defer func() { <-gate }()
					commit, cancel := context.WithTimeout(prepare, a.config.DBTimeout)
					defer cancel()
					old := a.motion.Current(id.User)
					if err := a.flushPrevious(commit, id, f); err != nil {
						if old != nil {
							old.Thaw()
						}
						return err
					}
					var e error
					welcome, e = a.visits.Activate(commit, id, in, localScene, f)
					if e != nil {
						if old != nil {
							old.Thaw()
						}
						return e
					}
					welcome.Speed = a.config.MoveSpeed
					pilot, e = a.motion.Join(movement.Stamp{Actor: welcome.Actor, Nickname: welcome.Nickname, User: id.User, Visit: welcome.Visit, Epoch: welcome.Epoch, Generation: welcome.Generation, Version: welcome.PositionVersion}, localScene, welcome.X, welcome.Y)
					if e == nil {
						pilot.Permit(time.Now().Add(2 * time.Second))
					}
					return e
				}()
			}
		}
	}
	finish()
	if pinRelease != nil {
		defer func() { pinRelease() }()
	}
	out := &stream{c: c, ctx: r.Context(), metrics: &a.transport}
	if pilot != nil {
		out.visit = welcome.Visit
		out.epoch = welcome.Epoch
	}
	send := out.send
	if err != nil {
		closeCause = "visit_activation_failed"
		closeError = err
		_ = send("error", map[string]string{"code": publicError(err)})
		return
	}
	epochForLog = welcome.Epoch
	f := activeFence
	ended := false
	defer func() { a.disconnectMotion(id, welcome, f, pilot, gate, &ended) }()
	if err = send("welcome", welcome); err != nil {
		closeCause = "welcome_write_failed"
		closeError = err
		return
	}
	if err = out.snapshot(pilot); err != nil {
		closeCause = "snapshot_write_failed"
		closeError = err
		return
	}
	established = true
	run, stop := context.WithCancel(r.Context())
	defer stop()
	received := make(chan frame, 1)
	readErrors := make(chan error, 1)
	go func() {
		for {
			var msg frame
			if e := wsjson.Read(run, c, &msg); e != nil {
				select {
				case readErrors <- e:
				case <-run.Done():
				}
				return
			}
			select {
			case received <- msg:
			case <-run.Done():
				return
			}
		}
	}()

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	checkpoint := time.NewTicker(5*time.Second + time.Duration(id.User%500)*time.Millisecond)
	defer checkpoint.Stop()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	var lastResync time.Time
	transition := &crossing{}
	defer transition.close()
	var seq int64 = 1
	var accepted, reported int64 = -1, -1
	tokens, lastRefill := 20.0, time.Now()
	var returnPrepared time.Time
	for {
		select {
		case <-run.Done():
			closeCause = "connection_context_canceled"
			closeError = run.Err()
			return
		case readErr := <-readErrors:
			closeCause = "read_error"
			closeError = readErr
			return
		case <-pilot.Done():
			closeCause = "session_replaced"
			_ = send("error", map[string]string{"code": "session_replaced"})
			return
		case state := <-pilot.Updates():
			if state.Error != "" && reported != state.Generation {
				reported = state.Generation
				if send("error", map[string]any{"code": state.Error, "seq": state.Seq}) != nil {
					return
				}
			}
			if len(state.Path) > 0 && accepted != state.Generation {
				accepted = state.Generation
				if send("path.accepted", state) != nil {
					return
				}
			}
		case loaded := <-transition.result():
			offer := transition.offer
			if loaded.err != nil {
				transition.blocked = offer.Neighbor
				transition.close()
				pilot.Thaw()
				if send("transition.cancelled", map[string]string{"reason": "transition_load_failed"}) != nil {
					return
				}
			} else {
				offer.pin = loaded.pin
				offer.Ready = true
				if send("transition.offer", offer) != nil {
					return
				}
			}
		case world := <-pilot.Presence():
			if err = out.delta(pilot, world); err != nil {
				closeCause = "stream_write_failed"
				closeError = err
				return
			}
		case <-ping.C:
			ctx, done := context.WithTimeout(run, 5*time.Second)
			err := c.Ping(ctx)
			done()
			if err != nil {
				closeCause = "ping_failed"
				closeError = err
				return
			}
		case msg := <-received:
			now := time.Now()
			tokens = min(20, tokens+now.Sub(lastRefill).Seconds()*10)
			lastRefill = now
			if tokens < 1 {
				_ = send("error", map[string]string{"code": "move_rate_limited"})
				return
			}
			tokens--
			if msg.V == 1 && msg.Visit == welcome.Visit && msg.Type == "transition.confirm" && transition.completed != nil && (msg.Epoch == welcome.Epoch || msg.Epoch == transition.previousEpoch) {
				var command transitionCommand
				if json.Unmarshal(msg.Payload, &command) == nil && command.Offer == transition.completedOffer && command.Operation == transition.completed.Operation {
					check, done := context.WithTimeout(run, a.config.DBTimeout)
					err := a.visits.Check(check, id, welcome, f)
					done()
					if err != nil {
						_ = send("error", map[string]string{"code": publicError(err)})
						return
					}
					if send("transition.complete", transition.completed) != nil {
						return
					}
					continue
				}
			}
			// Commands already queued by the browser before a completed crossing cannot
			// move the character in its new scene or tear down the new controller.
			if msg.V == 1 && msg.Visit == welcome.Visit && msg.Epoch == transition.previousEpoch && msg.Epoch < welcome.Epoch {
				continue
			}
			if msg.V != 1 || msg.Visit != welcome.Visit || msg.Epoch != welcome.Epoch {
				closeCause = "invalid_frame_epoch"
				return
			}
			if msg.Seq <= seq {
				continue
			}
			seq = msg.Seq
			check, done := context.WithTimeout(run, a.config.DBTimeout)
			err = a.visits.Check(check, id, welcome, f)
			done()
			if err != nil {
				closeCause = "command_visit_check_failed"
				closeError = err
				_ = send("error", map[string]string{"code": publicError(err)})
				return
			}
			pilot.Permit(time.Now().Add(2 * time.Second))
			if !returnPrepared.IsZero() && (msg.Type == "move" || msg.Type == "transition.confirm") {
				_ = send("error", map[string]string{"code": "return_pending"})
				continue
			}
			switch msg.Type {
			case "move":
				var goal struct {
					X int `json:"x_q"`
					Y int `json:"y_q"`
				}
				d := json.NewDecoder(bytes.NewReader(msg.Payload))
				d.DisallowUnknownFields()
				if d.Decode(&goal) != nil {
					_ = send("error", map[string]string{"code": "invalid_target"})
					continue
				}
				if transition.offer != nil {
					transition.blocked = transition.offer.Neighbor
					transition.close()
					pilot.Thaw()
					if send("transition.cancelled", map[string]string{"reason": "move"}) != nil {
						return
					}
				}
				if p, ok := visits.Portal(pilot.Scene(), transition.blocked); ok && visits.Near(p, goal.X, goal.Y) {
					transition.blocked = 0
				}
				if err = pilot.Move(seq, goal.X, goal.Y); err != nil {
					if send("error", map[string]any{"code": err.Error(), "seq": seq}) != nil {
						return
					}
				}
			case "transition.cancel":
				var input transitionCommand
				if json.Unmarshal(msg.Payload, &input) != nil || transition.offer == nil || input.Offer != transition.offer.ID {
					if send("error", map[string]string{"code": "offer_expired"}) != nil {
						return
					}
					continue
				}
				transition.blocked = transition.offer.Neighbor
				transition.close()
				pilot.Thaw()
				if send("transition.cancelled", map[string]string{"reason": "cancelled"}) != nil {
					return
				}
			case "transition.confirm":
				var input transitionCommand
				decoder := json.NewDecoder(bytes.NewReader(msg.Payload))
				decoder.DisallowUnknownFields()
				if decoder.Decode(&input) != nil || !access.ValidUUID(input.Operation) || transition.offer == nil || input.Offer != transition.offer.ID || !transition.offer.Ready {
					if send("error", map[string]string{"code": "offer_expired"}) != nil {
						return
					}
					continue
				}
				commit, cancel := context.WithTimeout(run, a.config.DBTimeout)
				replacement, next, err := a.commitCrossing(commit, id, welcome, f, pilot, transition.offer, input.Operation, gate)
				cancel()
				if err != nil {
					transition.blocked = transition.offer.Neighbor
					transition.close()
					pilot.Thaw()
					if send("transition.cancelled", map[string]string{"reason": publicError(err)}) != nil {
						return
					}
					// A transport/commit error can have an unknown outcome. Stop this
					// controller and resolve the committed location through resume.
					if replacement == nil || next.Epoch != welcome.Epoch || !(errors.Is(err, access.ErrDenied) || errors.Is(err, visits.ErrOffer) || errors.Is(err, visits.ErrOperation)) {
						return
					}
					continue
				}
				transition.previousEpoch = welcome.Epoch
				transition.completed = &transitionComplete{input.Operation, next}
				transition.completedOffer = input.Offer
				pinRelease()
				pinRelease = transition.offer.pin.Release
				transition.offer.pin = nil
				transition.close()
				pilot = replacement
				welcome = next
				out.epoch = next.Epoch
				accepted = -1
				reported = -1
				// Suppress the reciprocal edge until the arrival zone has been left.
				transition.blocked = localScene.Key().Sector
				localScene = pilot.Scene()
				if send("transition.complete", transition.completed) != nil || out.snapshot(pilot) != nil {
					return
				}
			case "resync":
				var input struct {
					Last int64 `json:"last_server_seq"`
				}
				decoder := json.NewDecoder(bytes.NewReader(msg.Payload))
				decoder.DisallowUnknownFields()
				if decoder.Decode(&input) != nil || input.Last < 0 {
					if send("error", map[string]string{"code": "invalid_resync"}) != nil {
						return
					}
					continue
				}
				if time.Since(lastResync) < time.Second {
					continue
				}
				lastResync = time.Now()
				if out.snapshot(pilot) != nil {
					return
				}
			case "return.prepare":
				var input struct {
					Operation string `json:"operation"`
				}
				if json.Unmarshal(msg.Payload, &input) != nil || !access.ValidUUID(input.Operation) {
					_ = send("error", map[string]string{"code": "invalid_return"})
					continue
				}
				if transition.offer != nil || pilot.Snapshot().Moving {
					_ = send("error", map[string]string{"code": "return_stop_required"})
					continue
				}
				save, done := context.WithTimeout(run, a.config.DBTimeout)
				var proof visits.ReturnProof
				if err = takeGate(save, gate); err == nil {
					pilot.Freeze()
					err = a.saveMotion(save, id, pilot, f, false)
					if err == nil {
						stamp := pilot.Stamp()
						proof, err = a.visits.PrepareReturn(save, id, visits.Welcome{Visit: stamp.Visit, Epoch: stamp.Epoch, Generation: stamp.Generation, PositionVersion: stamp.Version}, f, pilot.Scene(), input.Operation)
					}
					<-gate
				}
				done()
				if err != nil {
					pilot.Thaw()
					_ = send("error", map[string]string{"code": publicError(err)})
					continue
				}
				returnPrepared = proof.Expires
				if send("return.authorized", proof) != nil {
					return
				}
			case "leave":
				// The old command must never be an unrestricted alternate return route.
				_ = send("error", map[string]string{"code": "return_authorization_required"})

			default:
				_ = send("error", map[string]string{"code": "unsupported_command"})
				return
			}
		case <-tick.C:
			if !returnPrepared.IsZero() && time.Now().After(returnPrepared) {
				returnPrepared = time.Time{}
				pilot.Thaw()
			}
			if transition.offer != nil && time.Now().After(transition.offer.Expires) {
				transition.blocked = transition.offer.Neighbor
				transition.close()
				pilot.Thaw()
				if send("transition.cancelled", map[string]string{"reason": "offer_expired"}) != nil {
					return
				}
			}
			check, done := context.WithTimeout(run, a.config.DBTimeout)
			err = a.visits.Check(check, id, welcome, f)
			done()
			if err != nil {
				closeCause = "heartbeat_visit_check_failed"
				closeError = err
				_ = send("error", map[string]string{"code": publicError(err)})
				return
			}
			pilot.Permit(time.Now().Add(2 * time.Second))
			offered, e := a.offerTransition(run, id, pilot, transition)
			if e != nil {
				if send("error", map[string]string{"code": publicError(e)}) != nil {
					return
				}
			} else if offered {
				if send("transition.offer", transition.offer) != nil {
					return
				}
			}
		case <-checkpoint.C:
			save, done := context.WithTimeout(run, a.config.DBTimeout)
			if err = takeGate(save, gate); err == nil {
				err = a.saveMotion(save, id, pilot, f, false)
				<-gate
			}
			done()
			if err != nil {
				closeCause = "checkpoint_failed"
				closeError = err
				_ = send("error", map[string]string{"code": "save_failed"})
				return
			}
		}
	}
}
