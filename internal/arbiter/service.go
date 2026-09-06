package arbiter

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/TrueBlocks/rulesforpennies.io/internal/ratelimit"
	"github.com/TrueBlocks/rulesforpennies.io/internal/rulesdb"
	"github.com/TrueBlocks/rulesforpennies.io/internal/suggestions"
	"github.com/TrueBlocks/trueblocks-art/packages/ai"
)

const adminToken = "penny1793"

var substantiveRuleRe = regexp.MustCompile(`§[2-6]\.\d`)

type Service struct {
	provider       ai.Provider
	promptTemplate string
	rulesDB        *rulesdb.DB
	limiter        *ratelimit.Limiter
	suggestions    *suggestions.Store
}

func New(provider ai.Provider, promptTemplate string, db *rulesdb.DB, limiter *ratelimit.Limiter, sg *suggestions.Store) *Service {
	return &Service{
		provider:       provider,
		promptTemplate: promptTemplate,
		rulesDB:        db,
		limiter:        limiter,
		suggestions:    sg,
	}
}

type rulingRequest struct {
	Situation string `json:"situation"`
}

type ruleCitation struct {
	Code    string `json:"code"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

type rulingResponse struct {
	Ruling    string         `json:"ruling"`
	Rules     []ruleCitation `json:"rules,omitempty"`
	Cost      float64        `json:"cost"`
	Deflected bool           `json:"deflected,omitempty"`
	Slug      string         `json:"slug,omitempty"`
	Throttled bool           `json:"throttled,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func (s *Service) HandleRuling(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	sessionToken := r.Header.Get("X-Session-Token")
	log.Printf("[request] start from session=%s at %s", sessionToken, start.Format(time.RFC3339Nano))
	if sessionToken == "" {
		writeError(w, http.StatusBadRequest, "missing session token", "missing_token")
		return
	}

	var req rulingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "bad_request")
		return
	}
	req.Situation = strings.TrimSpace(req.Situation)
	if req.Situation == "" {
		writeError(w, http.StatusBadRequest, "situation is required", "empty_situation")
		return
	}
	if len(req.Situation) > 2000 {
		writeError(w, http.StatusBadRequest, "situation too long (max 2000 characters)", "too_long")
		return
	}
	log.Printf("[request] decoded situation=%q len=%d elapsed=%s", req.Situation, len(req.Situation), time.Since(start))

	if blocked, reason := checkInputFilters(req.Situation); blocked {
		log.Printf("[request] blocked input from %s: %s", sessionToken, reason)
		writeJSON(w, http.StatusOK, rulingResponse{
			Ruling:    "The arbiter declines to entertain motions that fall outside the scope of penny-related jurisprudence. The petitioner is reminded that this body's jurisdiction is limited to currency encounters as defined in the Official Code, available at https://rules-for-pennies.stonylanepress.com",
			Deflected: true,
		})
		return
	}

	adminHeader := r.Header.Get("X-Admin-Token")
	isAdmin := adminHeader == adminToken

	status := s.limiter.Check(sessionToken)
	log.Printf("[request] limiter status soft=%v hard=%v remaining=%d elapsed=%s", status.SoftCapped, status.HardCapped, status.Remaining, time.Since(start))
	if status.HardCapped && !isAdmin {
		log.Printf("[request] hard cap hit for session=%s", sessionToken)
		writeJSON(w, http.StatusOK, rulingResponse{
			Ruling: "The arbiter has retired for the day. Court reconvenes tomorrow. In the interim, the petitioner is directed to consult the Official Code at https://rules-for-pennies.stonylanepress.com — wherein one may find the complete and unabridged text of every ruling, provision, and amendment herein referenced.",
		})
		return
	}

	rules, err := s.rulesDB.Search(req.Situation, 5)
	if err != nil {
		log.Printf("[request] rules search error: %v", err)
		rules, _ = s.rulesDB.Foundational()
	}
	log.Printf("[request] rules search returned %d rules elapsed=%s", len(rules), time.Since(start))

	prompt := s.buildPrompt(rules)
	log.Printf("[request] calling OpenAI soft=%v elapsed=%s", status.SoftCapped, time.Since(start))
	ruling, cost, throttled, err := s.callOpenAI(prompt, req.Situation, status.SoftCapped)
	if err != nil {
		log.Printf("[request] OpenAI error after %s: %v", time.Since(start), err)
		writeError(w, http.StatusInternalServerError, "the arbiter is temporarily indisposed", "api_error")
		return
	}
	log.Printf("[request] OpenAI returned cost=%.6f throttled=%v elapsed=%s", cost, throttled, time.Since(start))

	if leaked := checkOutputFilters(ruling); leaked {
		log.Printf("[request] output filter triggered for session %s", sessionToken)
		ruling = "The arbiter's ruling has been sealed pending review. The petitioner is directed to the Official Code at https://rules-for-pennies.stonylanepress.com for authoritative guidance on this matter."
	}

	s.limiter.Record(sessionToken, cost)

	// Detect deflection: no citation from substantive rule sections (§2–§6)
	deflected := !substantiveRuleRe.MatchString(ruling)
	ruling = strings.ReplaceAll(ruling, "[NO_JURISDICTION] ", "")
	ruling = strings.ReplaceAll(ruling, "[NO_JURISDICTION]", "")

	var citations []ruleCitation
	for _, r := range rules {
		citations = append(citations, ruleCitation{
			Code:    r.Code,
			Title:   r.Title,
			Summary: r.Summary,
		})
	}

	// Save ruling to shared store
	slug := makeSlug(req.Situation)
	rulesJSON, _ := json.Marshal(citations)
	if err := s.suggestions.SaveRuling(slug, req.Situation, ruling, rulesJSON); err != nil {
		log.Printf("failed to save ruling: %v", err)
	}

	writeJSON(w, http.StatusOK, rulingResponse{
		Ruling:    ruling,
		Rules:     citations,
		Cost:      cost,
		Deflected: deflected,
		Slug:      slug,
		Throttled: throttled,
	})
	log.Printf("[request] response sent deflected=%v throttled=%v total_elapsed=%s", deflected, throttled, time.Since(start))
}

func (s *Service) buildPrompt(rules []rulesdb.Rule) string {
	corpus := rulesdb.FormatForPrompt(rules)
	return strings.Replace(s.promptTemplate, "{{RULES_CORPUS}}", corpus, 1)
}

func (s *Service) callOpenAI(systemPrompt, situation string, addDelay bool) (string, float64, bool, error) {
	start := time.Now()
	throttled := false
	if addDelay {
		throttled = true
		log.Printf("[openai] soft cap reached: applying 500ms throttle")
		time.Sleep(500 * time.Millisecond)
	}

	prompt := systemPrompt + "\n\n" + situation
	result, err := s.provider.Call(context.Background(), "gpt-4o", prompt, ai.CallOptions{
		MaxTokens: 500,
		Timeout:   60 * time.Second,
	})
	if err != nil {
		return "", 0, false, fmt.Errorf("api call: %w", err)
	}

	log.Printf("[openai] completed tokens_in=%d tokens_out=%d cost=%.6f total_elapsed=%s", result.InputTokens, result.OutputTokens, result.Cost, time.Since(start))

	return result.Content, result.Cost, throttled, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg, code string) {
	writeJSON(w, status, errorResponse{Error: msg, Code: code})
}

var nonAlpha = regexp.MustCompile(`[^a-z0-9]+`)

func makeSlug(text string) string {
	s := strings.ToLower(text)
	s = nonAlpha.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 30 {
		s = s[:30]
		s = strings.TrimRight(s, "-")
	}
	h := fnv.New32a()
	h.Write([]byte(text))
	return fmt.Sprintf("%s-%04x", s, h.Sum32()&0xFFFF)
}

func (s *Service) HandleSuggest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Slug      string `json:"slug"`
		Rationale string `json:"rationale"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request", "bad_request")
		return
	}
	req.Slug = strings.TrimSpace(req.Slug)
	req.Rationale = strings.TrimSpace(req.Rationale)
	if req.Slug == "" {
		writeError(w, http.StatusBadRequest, "slug is required", "empty_slug")
		return
	}

	if err := s.suggestions.Propose(req.Slug, req.Rationale); err != nil {
		log.Printf("propose error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to propose", "save_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Service) HandleListRulings(w http.ResponseWriter, r *http.Request) {
	items, err := s.suggestions.List()
	if err != nil {
		log.Printf("rulings list error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list rulings", "list_error")
		return
	}
	if items == nil {
		items = []suggestions.Ruling{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Service) HandleDeleteRuling(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Admin-Token") != adminToken {
		writeError(w, http.StatusForbidden, "forbidden", "forbidden")
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id", "bad_id")
		return
	}

	if err := s.suggestions.Delete(id); err != nil {
		log.Printf("ruling delete error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to delete", "delete_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
