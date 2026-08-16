package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"leetoffice/internal/store"
)

// Default Ollama endpoint and embedding model (BUILD_SPEC §2, §8.3).
const (
	defaultOllamaURL  = "http://127.0.0.1:11434"
	defaultEmbedModel = "nomic-embed-text"
	embedBatch        = 64
	availableTimeout  = 2 * time.Second
	embedHTTPTimeout  = 60 * time.Second
)

// Ollama is the optional local embeddings backend. It is reached over plain
// HTTP; when it is unavailable, or anything fails, searches fall back to the
// offline keyword Search so the search contract always holds (RUNBOOK §6).
type Ollama struct {
	BaseURL string // default http://127.0.0.1:11434
	Model   string // default nomic-embed-text
}

// Available probes GET /api/tags with a short timeout.
func (o *Ollama) Available() bool {
	ctx, cancel := context.WithTimeout(context.Background(), availableTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.base()+"/api/tags", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// SearchSemantic embeds the query and every candidate block via Ollama
// (batched /api/embed calls), ranks by cosine similarity, and applies the
// memory boost. On unavailability or any error it falls back to keyword
// Search (D17).
func (o *Ollama) SearchSemantic(s *store.Store, query, typ string, tags []string, limit int) ([]Hit, error) {
	if limit <= 0 {
		limit = defaultLimit
	}
	fallback := func() ([]Hit, error) { return Search(s, query, typ, tags, limit) }
	if !o.Available() {
		return fallback()
	}
	terms := tokenize(query)
	if len(terms) == 0 {
		return nil, nil
	}
	cands, err := collectCandidates(s, typ, tags)
	if err != nil {
		return nil, err
	}
	if len(cands) == 0 {
		return nil, nil
	}
	texts := make([]string, 0, len(cands)+1)
	texts = append(texts, query)
	for _, c := range cands {
		texts = append(texts, c.text)
	}
	vecs, err := o.embed(texts)
	if err != nil {
		return fallback()
	}
	q := vecs[0]
	var hits []Hit
	for i, c := range cands {
		score := cosine(q, vecs[i+1])
		if score <= 0 {
			continue
		}
		if c.boosted {
			score *= MemoryBoost
		}
		hits = append(hits, Hit{
			DocID:   c.docID,
			BlockID: c.blockID,
			Slug:    c.slug,
			Title:   c.title,
			Snippet: snippet(c.text, terms),
			Score:   score,
		})
	}
	return sortHits(hits, limit), nil
}

// base returns the normalized endpoint URL.
func (o *Ollama) base() string {
	u := o.BaseURL
	if u == "" {
		u = defaultOllamaURL
	}
	return strings.TrimSuffix(u, "/")
}

// model returns the embedding model name.
func (o *Ollama) model() string {
	if o.Model == "" {
		return defaultEmbedModel
	}
	return o.Model
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
}

// embed sends the texts (query first is the caller's convention) to
// /api/embed in batches and returns one vector per text, in order.
func (o *Ollama) embed(texts []string) ([][]float64, error) {
	client := &http.Client{Timeout: embedHTTPTimeout}
	var out [][]float64
	for i := 0; i < len(texts); i += embedBatch {
		end := min(i+embedBatch, len(texts))
		body, err := json.Marshal(embedRequest{Model: o.model(), Input: texts[i:end]})
		if err != nil {
			return nil, err
		}
		resp, err := client.Post(o.base()+"/api/embed", "application/json", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("ollama embed: status %d", resp.StatusCode)
		}
		var er embedResponse
		err = json.NewDecoder(resp.Body).Decode(&er)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if len(er.Embeddings) != end-i {
			return nil, fmt.Errorf("ollama embed: %d embeddings for %d inputs", len(er.Embeddings), end-i)
		}
		for _, v := range er.Embeddings {
			if len(v) == 0 {
				return nil, fmt.Errorf("ollama embed: empty vector")
			}
		}
		out = append(out, er.Embeddings...)
	}
	return out, nil
}

// cosine computes the cosine similarity of two vectors (0 on mismatch).
func cosine(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
