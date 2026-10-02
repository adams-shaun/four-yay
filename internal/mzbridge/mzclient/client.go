// Package mzclient is the HTTP client for MageZero's inference server.
//
// It lives apart from internal/mzbridge because it reads a clock (the
// micro-batch flush timer and request timeouts) and the bridge's encoders
// must not: nothing here reaches an event, a feature id or a shard.
package mzclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/adams-shaun/gorge/internal/mzbridge"
)

// Client talks to MageZero's inference server (server.py): POST /evaluate
// with a MessagePack body, GET /healthz. Evaluate is safe for concurrent use
// and micro-batches: bags from concurrent callers are combined into one
// request, sent when MaxBatch bags are waiting or FlushInterval after the
// first of them arrived, whichever is first. The defaults are the Java
// client's (RemoteModelEvaluator.java:49-50): 4 bags, 2.5 ms.
type Client struct {
	base     string
	http     *http.Client
	maxBatch int
	interval time.Duration

	in       chan *pendingEval
	inflight chan struct{}
	done     chan struct{}
	closing  sync.Once
	wg       sync.WaitGroup
}

// Options tunes a Client; the zero value is the upstream defaults.
type Options struct {
	// MaxBatch is the most bags one request carries (default 4).
	MaxBatch int
	// FlushInterval is how long the first waiting bag waits for company
	// (default 2.5 ms).
	FlushInterval time.Duration
	// MaxInFlight caps concurrent requests (default 16, the Java client's
	// MAX_CONCURRENT_CALLS). server.py answers MZ_SERVER_THREADS (default
	// 6) requests at a time and queues the rest.
	MaxInFlight int
	// HTTPClient replaces the default client (keep-alive, 60 s timeout).
	HTTPClient *http.Client
}

// ErrClosed is returned by Evaluate once Close has been called.
var ErrClosed = errors.New("mzclient: closed")

// ErrEmptyBag is returned for a state with no feature ids. It is refused
// before anything is sent because of what server.py (pinned 521a8bd) does
// with it: when every bag of a server-side batch is empty once the model's
// vocabulary has dropped the ids it does not know, the forward pass raises
// IndexError in the server's only worker thread (worker_loop, server.py:126;
// model.py:159), the thread dies, and every later /evaluate blocks forever
// while /healthz keeps answering ok. Observed against the real server.
//
// The client can only catch the literally empty bag. A bag made entirely of
// ids outside the model's vocabulary does the same thing when it is alone in
// its batch, and when it shares a batch it gets a finite, state-independent
// output (the heads applied to a zero vector) with no error. The caller must
// prevent it: an encoded game state always carries ids, such as the decision
// type, that every state of the training window shares.
var ErrEmptyBag = errors.New("mzclient: empty feature bag")

type pendingEval struct {
	ids []int32
	out chan evalResult
}

type evalResult struct {
	eval mzbridge.Evaluation
	err  error
}

// New returns a client for the server at baseURL
// ("http://127.0.0.1:50052"). It does not contact the server; call Healthz.
func New(baseURL string, opt Options) *Client {
	c := &Client{
		base:     strings.TrimRight(baseURL, "/"),
		http:     opt.HTTPClient,
		maxBatch: opt.MaxBatch,
		interval: opt.FlushInterval,
		in:       make(chan *pendingEval),
		done:     make(chan struct{}),
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: 60 * time.Second}
	}
	if c.maxBatch <= 0 {
		c.maxBatch = 4
	}
	if c.interval <= 0 {
		c.interval = 2500 * time.Microsecond
	}
	if opt.MaxInFlight <= 0 {
		opt.MaxInFlight = 16
	}
	c.inflight = make(chan struct{}, opt.MaxInFlight)
	c.wg.Add(1)
	go c.batcher()
	return c
}

// Close stops the batcher, fails callers still waiting to be batched and
// waits for requests in flight.
func (c *Client) Close() {
	c.closing.Do(func() { close(c.done) })
	c.wg.Wait()
}

// Healthz checks GET /healthz.
func (c *Client) Healthz(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mzclient: healthz: HTTP %d", resp.StatusCode)
	}
	return nil
}

// Evaluate returns the network's output for one state's feature ids (raw
// hashed ids; the server maps them to its vocabulary and drops the ones it
// does not know). It blocks until the batch it joined is answered.
func (c *Client) Evaluate(ctx context.Context, ids []int32) (mzbridge.Evaluation, error) {
	if len(ids) == 0 {
		return mzbridge.Evaluation{}, ErrEmptyBag
	}
	p := &pendingEval{ids: ids, out: make(chan evalResult, 1)}
	select {
	case c.in <- p:
	case <-c.done:
		return mzbridge.Evaluation{}, ErrClosed
	case <-ctx.Done():
		return mzbridge.Evaluation{}, ctx.Err()
	}
	select {
	case r := <-p.out:
		return r.eval, r.err
	case <-ctx.Done():
		return mzbridge.Evaluation{}, ctx.Err()
	}
}

// EvaluateBatch sends bags as one request, bypassing the micro-batcher, and
// returns one mzbridge.Evaluation per bag in order.
func (c *Client) EvaluateBatch(ctx context.Context, bags [][]int32) ([]mzbridge.Evaluation, error) {
	if len(bags) == 0 {
		return nil, nil
	}
	for i, bag := range bags {
		if len(bag) == 0 {
			return nil, fmt.Errorf("%w (bag %d of %d)", ErrEmptyBag, i, len(bags))
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/evaluate",
		bytes.NewReader(mzbridge.AppendEvaluateRequest(nil, bags)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-msgpack")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mzclient: evaluate: HTTP %d", resp.StatusCode)
	}
	evals, err := mzbridge.DecodeEvaluateResponse(body, len(bags))
	if err != nil {
		return nil, err
	}
	for i := range evals {
		if v := evals[i].Value; v != v {
			// never observed from server.py; a corrupt checkpoint would
			// do it, and a NaN must not reach the search as a value
			return nil, fmt.Errorf("mzclient: evaluate: NaN value for bag %d of %d", i, len(bags))
		}
	}
	return evals, nil
}

func (c *Client) batcher() {
	defer c.wg.Done()
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	for {
		var batch []*pendingEval
		select {
		case p := <-c.in:
			batch = append(batch, p)
		case <-c.done:
			return
		}
		timer.Reset(c.interval)
	collect:
		for len(batch) < c.maxBatch {
			select {
			case p := <-c.in:
				batch = append(batch, p)
			case <-timer.C:
				break collect
			case <-c.done:
				fail(batch, ErrClosed)
				return
			}
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		select {
		case c.inflight <- struct{}{}:
		case <-c.done:
			fail(batch, ErrClosed)
			return
		}
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			defer func() { <-c.inflight }()
			c.send(batch)
		}()
	}
}

func fail(batch []*pendingEval, err error) {
	for _, p := range batch {
		p.out <- evalResult{err: err}
	}
}

func (c *Client) send(batch []*pendingEval) {
	bags := make([][]int32, len(batch))
	for i, p := range batch {
		bags[i] = p.ids
	}
	// the batch outlives any one caller's context: the others still want it
	evals, err := c.EvaluateBatch(context.Background(), bags)
	if err != nil {
		fail(batch, err)
		return
	}
	for i, p := range batch {
		p.out <- evalResult{eval: evals[i]}
	}
}
