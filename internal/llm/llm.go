// Package llm is the thin, provider-agnostic layer Tulipe uses to talk to a
// translation model. It exposes exactly what a translator needs — a system
// prompt, a user prompt, and a JSON answer — and nothing else.
package llm

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/blkmlo/tulipe/internal/i18n"
)

// Request is one completion call.
type Request struct {
	System    string
	User      string
	MaxTokens int64
	// Schema, when set, asks the provider to constrain its answer to this JSON
	// schema. Providers that cannot honour it fall back to plain text and the
	// caller parses the JSON itself.
	Schema map[string]any
}

// Usage reports what a call consumed. Reported is false when the provider sent
// no usage block: the counters are then meaningless and must be displayed as
// unavailable rather than as zero.
type Usage struct {
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	Reported         bool
}

// Add accumulates usage, keeping Reported false unless every contribution was
// itself reported.
func (u *Usage) Add(o Usage) {
	if !o.Reported {
		return
	}
	u.InputTokens += o.InputTokens
	u.OutputTokens += o.OutputTokens
	u.CacheReadTokens += o.CacheReadTokens
	u.CacheWriteTokens += o.CacheWriteTokens
	u.Reported = true
}

// Response is the model's answer.
type Response struct {
	Text  string
	Model string
	Usage Usage
}

// Provider is a translation backend.
type Provider interface {
	// ID identifies the backend, e.g. "anthropic".
	ID() string
	// Model is the model identifier in use.
	Model() string
	// Complete runs one request to completion.
	Complete(ctx context.Context, req Request) (*Response, error)
}

// SegmentRequest asks a backend to translate a batch of segments directly,
// without being prompted.
type SegmentRequest struct {
	Segments []string
	// TargetCode and SourceCode are language tags; an empty source asks the
	// service to detect it.
	TargetCode string
	SourceCode string
	// Markup says the segments carry inline XML tags that must survive.
	Markup bool
}

// SegmentResponse holds one translation per requested segment.
type SegmentResponse struct {
	Translations []string
	Model        string
	Usage        Usage
}

// DirectTranslator is implemented by backends that translate natively rather
// than by being instructed — DeepL, for one. When a provider implements it, the
// translator hands over the segments as they are and skips prompt building and
// JSON parsing entirely, which removes every failure mode that comes with them.
type DirectTranslator interface {
	TranslateSegments(ctx context.Context, req SegmentRequest) (*SegmentResponse, error)
}

// ModelLister is implemented by backends that can say which models they serve.
// Asking the service beats keeping a list that goes stale.
type ModelLister interface {
	ListModels(ctx context.Context) ([]string, error)
}

// APIError is a non-2xx answer from a provider.
type APIError struct {
	Provider string
	Status   int
	Type     string
	Message  string
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: HTTP %d", e.Provider, e.Status)
	if e.Type != "" {
		fmt.Fprintf(&b, " (%s)", e.Type)
	}
	if e.Message != "" {
		fmt.Fprintf(&b, ": %s", e.Message)
	}
	return b.String()
}

// Retryable reports whether the request has a chance of succeeding as-is on a
// later attempt: rate limits, overloads, and transient server failures.
func (e *APIError) Retryable() bool {
	switch e.Status {
	case 408, 409, 429:
		return true
	}
	return e.Status >= 500
}

// messageError is a sentinel whose text is looked up when it is read, not when
// the program starts. A package-level errors.New would freeze its wording in
// whatever language happened to be current at initialisation — that is, always
// the default one.
type messageError struct{ key string }

func (e messageError) Error() string { return i18n.T(e.key) }

// ErrTruncated means the model hit its output ceiling before finishing. The
// answer is unusable; the caller should retry with fewer segments. It stays
// comparable, so errors.Is keeps working.
var ErrTruncated error = messageError{"llm.err.truncated"}

// RefusalError means the model declined to answer.
type RefusalError struct {
	Category    string
	Explanation string
}

func (e *RefusalError) Error() string {
	msg := i18n.T("llm.err.refusal")
	if e.Category != "" {
		msg += " (" + e.Category + ")"
	}
	if e.Explanation != "" {
		msg += ": " + e.Explanation
	}
	return msg
}

// Retryable reports whether an error is worth retrying unchanged.
func Retryable(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var api *APIError
	if errors.As(err, &api) {
		return api.Retryable()
	}
	var refusal *RefusalError
	if errors.As(err, &refusal) {
		return false
	}
	if errors.Is(err, ErrTruncated) {
		return false
	}
	// Transport failures (connection reset, DNS hiccup, timeout) are worth
	// another attempt.
	return true
}

// Fatal reports whether an error will keep happening however long we wait:
// wrong credentials, no permission, or a model or endpoint that does not
// exist. Sending more requests would only waste time and money.
func Fatal(err error) bool {
	var api *APIError
	if !errors.As(err, &api) {
		return false
	}
	switch api.Status {
	case 401, 403, 404:
		return true
	}
	return false
}

// RetryNotice describes an attempt that failed and is about to be retried.
type RetryNotice struct {
	Attempt int
	Wait    time.Duration
	Err     error
}

// Retry runs fn until it succeeds, exhausts attempts, or fails with an error
// that retrying cannot fix. Waits grow exponentially with jitter.
func Retry(ctx context.Context, attempts int, base time.Duration, notify func(RetryNotice), fn func() error) error {
	if attempts < 1 {
		attempts = 1
	}
	var err error
	for i := 1; i <= attempts; i++ {
		if err = fn(); err == nil {
			return nil
		}
		if i == attempts || !Retryable(err) {
			return err
		}
		wait := time.Duration(float64(base) * math.Pow(2, float64(i-1)))
		wait += time.Duration(rand.Int64N(int64(wait/2) + 1))
		if notify != nil {
			notify(RetryNotice{Attempt: i, Wait: wait, Err: err})
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	return err
}

// errorsAs is errors.As, kept local so provider files do not each import the
// errors package for a single call.
func errorsAs(err error, target any) bool { return errors.As(err, target) }
