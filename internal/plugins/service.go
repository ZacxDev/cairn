package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ZacxDev/cairn/internal/transcript"
)

// ErrForbidden is EVERY refusal the plugin API answers with its one 403 body (decision 9, clause j,
// T10): a capability the manifest does not hold, a type it holds no `output:write:<type>` for, a session the
// plugin is OFF for, and a session that is not stored. One value, so no caller can answer them
// differently.
var ErrForbidden = errors.New("forbidden")

// Service is what the plugin API and the browser surface decide from.
type Service struct {
	Registry *Registry
	Journal  Journal
	// Transcripts is the transcript store's read side; nil means none — every session is "not
	// stored" and every plugin read is refused.
	Transcripts *transcript.Store
	// Known reports whether the authority still holds a scope of that STORE name ([Effective]).
	Known func(scope string) bool
	// Now is the pod clock.
	Now func() time.Time
	// Log receives operator lines (a session left out of a decision); nil discards them.
	Log func(string)
}

func (s *Service) log(line string) {
	if s.Log != nil {
		s.Log(line)
	}
}

func (s *Service) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

// decider binds ONE journal read into the gate [transcript.Store.ForPlugin] asks.
func (s *Service) decider(plugin string) (transcript.Gate, error) {
	st, err := s.Journal.Read()
	if err != nil {
		return transcript.Gate{}, err
	}
	return s.deciderOver(st, plugin), nil
}

// deciderOver is the gate over one folded journal.
//
// 🔴 IT READS NO ARC REGISTRY. Which arcs speak for a session is the member snapshot each arc
// toggle carries ([State.ArcsCovering]); the live registration — whatever a writer of the home last
// `PUT` — is not an input to any plugin decision.
func (s *Service) deciderOver(st State, plugin string) transcript.Gate {
	known := s.Known
	if known == nil {
		known = func(string) bool { return false }
	}
	subject := func(meta *transcript.Meta, v []string) Subject {
		ids := append([]string{meta.Root}, meta.Children...)
		return Subject{Root: meta.Root, V: v, Arcs: st.ArcsCovering(plugin, ids)}
	}
	return transcript.Gate{
		On:      func(meta *transcript.Meta, v []string) bool { return Effective(st, plugin, subject(meta, v), known) },
		Settled: func(meta *transcript.Meta, v []string) bool { return Settled(st, plugin, subject(meta, v), known) },
	}
}

// Open returns the session's [transcript.View] when `plugin` is ON for it, and [ErrForbidden]
// otherwise — whatever the reason.
//
// 🔴 A DELETED SESSION IS THAT SAME REFUSAL, AND DELIBERATELY NOT A 404 (S6, clause l). The plan's
// clause (l) reads "the plugin's next transcript read is 404"; as built it is the ONE 403 body, the
// bytes a never-stored id and an OFF session get. A distinct 404 for "this id WAS stored and has
// been deleted" would be the existence oracle T10 closes — any plugin token could ask which session
// ids ever held a transcript, including sessions it was never ON for — and it buys a plugin
// nothing: the deleted session is gone from its pending list, and every refusal already means "stop
// working on this session". The plan's clause is amended to say so.
func (s *Service) Open(plugin, root string) (*transcript.View, error) {
	gate, err := s.decider(plugin)
	if err != nil {
		return nil, err
	}
	v, err := s.Transcripts.ForPlugin(root, gate)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, ErrForbidden
	}
	return v, nil
}

// latestOutput is `plugin`'s last-written output in a session, nil when it has none.
func latestOutput(v *transcript.View, plugin string) (*Output, error) {
	var last *Output
	err := v.Outputs(func(line []byte) error {
		var o Output
		if err := json.Unmarshal(line, &o); err != nil {
			return nil // a line this build cannot read is not this plugin's watermark
		}
		if o.Plugin == plugin {
			last = &o
		}
		return nil
	})
	return last, err
}

// Pending is decision 13's query for one plugin.
//
// 🔴 NO WALK WHEN NOTHING CAN BE ON, AND ONE WALK OTHERWISE (#270 round 1). With no scope `allowed`
// or `on` for the plugin ([State.AnyConsent]) the answer is empty without reading a single session;
// otherwise every stored root is decided over ONE whole-store walk ([transcript.Store.ForPlugins]).
// A root whose `meta.json` does not decode is left out (OFF) and logged; any other error fails the
// query — a broken store is never a confident empty list.
func (s *Service) Pending(e Entry, limit int) ([]PendingItem, error) {
	st, err := s.Journal.Read()
	if err != nil {
		return nil, err
	}
	if !st.AnyConsent(e.Name) {
		return []PendingItem{}, nil
	}
	roots, err := s.Transcripts.Roots()
	if err != nil {
		return nil, err
	}
	views, err := s.Transcripts.ForPlugins(roots, s.deciderOver(st, e.Name), func(root string, err error) {
		s.log(fmt.Sprintf("plugins: session %s is left out of the pending query: %v", root, err))
	})
	if err != nil {
		return nil, err
	}
	var cands []Candidate
	for _, v := range views {
		prev, err := latestOutput(v, e.Name)
		if err != nil {
			return nil, err
		}
		streams := map[string]int64{}
		for name, pos := range v.Meta.Streams {
			streams[name] = pos.SeqTo
		}
		cands = append(cands, Candidate{Root: v.Meta.Root, Streams: streams, UpdatedAt: v.Meta.UpdatedAt, Prev: prev})
	}
	return Pending(e, cands, s.now(), limit), nil
}

// WriteOutput validates, stamps and stores one output.
//
// 🔴 THE TYPE IS CHECKED AGAINST THE TOKEN'S MANIFEST BEFORE THE SESSION IS LOOKED AT, and the
// session's toggle after — both refusals are [ErrForbidden]. The watermark may not name a stream the
// session does not hold or a position past what is stored: a summary "of turns 1–N" where N was
// never uploaded is a claim about records nobody can read.
func (s *Service) WriteOutput(e Entry, env Envelope) (Output, error) {
	if !e.Writes(env.Type) {
		return Output{}, ErrForbidden
	}
	v, err := s.Open(e.Name, env.Session)
	if err != nil {
		return Output{}, err
	}
	for stream, seq := range env.InputWatermark {
		pos, ok := v.Meta.Streams[stream]
		if !ok || seq > pos.SeqTo {
			return Output{}, &WatermarkError{Stream: stream}
		}
	}
	out, err := Stamp(env, e, s.now())
	if err != nil {
		return Output{}, err
	}
	line, err := json.Marshal(out)
	if err != nil {
		return Output{}, err
	}
	if err := v.AppendOutput(e.Name, line); err != nil {
		if errors.Is(err, transcript.ErrNoSession) {
			return Output{}, ErrForbidden
		}
		return Output{}, err
	}
	return out, nil
}

// WatermarkError is an envelope whose watermark names a stream the session does not hold, or a
// position past what is stored. Only ever returned AFTER the toggle check admitted the plugin, so it
// tells the plugin nothing about a session it may not read.
type WatermarkError struct{ Stream string }

func (e *WatermarkError) Error() string {
	return fmt.Sprintf("input_watermark names stream %q past what is stored (or a stream the session does not hold)", e.Stream)
}

// ReadOutputs is every stored output of type `t` for a session, any plugin's — for a plugin holding
// `output:read:<t>` that is ON for the session.
func (s *Service) ReadOutputs(e Entry, root, t string) ([]Output, error) {
	if !IsOutputType(t) || !e.Has(CapOutputRead(t)) {
		return nil, ErrForbidden
	}
	v, err := s.Open(e.Name, root)
	if err != nil {
		return nil, err
	}
	out := []Output{}
	err = v.Outputs(func(line []byte) error {
		var o Output
		if json.Unmarshal(line, &o) == nil && o.Type == t {
			out = append(out, o)
		}
		return nil
	})
	return out, err
}
