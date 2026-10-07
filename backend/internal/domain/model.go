package domain

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"
)

type Amount int64

func (a Amount) MarshalJSON() ([]byte, error) { return json.Marshal(strconv.FormatInt(int64(a), 10)) }
func (a *Amount) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("minor_units must be an integer string")
	}
	n, e := strconv.ParseInt(s, 10, 64)
	*a = Amount(n)
	return e
}
func Add(a, b Amount) (Amount, error) {
	if (b > 0 && a > Amount(math.MaxInt64)-b) || (b < 0 && a < Amount(math.MinInt64)-b) {
		return 0, errors.New("amount overflow")
	}
	return a + b, nil
}
func Subtract(a, b Amount) (Amount, error) {
	if (b > 0 && a < Amount(math.MinInt64)+b) || (b < 0 && a > Amount(math.MaxInt64)+b) {
		return 0, errors.New("amount overflow")
	}
	return a - b, nil
}
func ID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	s := hex.EncodeToString(b)
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
}

type Fault struct {
	Code    string
	Message string
	Status  int
}

func (e *Fault) Error() string { return e.Code + ": " + e.Message }
func Invalid(s string) error   { return &Fault{"invalid_request", s, 422} }
func Conflict(s string) error  { return &Fault{"revision_conflict", s, 409} }
func Missing(s string) error   { return &Fault{"not_found", s, 404} }
func Unauthorized() error {
	return &Fault{"unauthorized", "有效访问令牌或账本权限不足", 401}
}

type Ledger struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Currency string    `json:"currency"`
	Cutover  time.Time `json:"cutover_time"`
	Version  int64     `json:"version"`
	Status   string    `json:"status"`
}
type Account struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Cash        bool   `json:"cash"`
	Initialized bool   `json:"initialized"`
	Revision    int64  `json:"revision"`
	Provider    string `json:"provider"`
	MaskedRef   string `json:"masked_ref"`
	Balance     Amount `json:"balance_minor"`
}
type Facts struct {
	Kind               string     `json:"kind"`
	Amount             Amount     `json:"amount_minor"`
	Currency           string     `json:"currency"`
	OccurredAt         *time.Time `json:"occurred_at"`
	TimePrecision      string     `json:"time_precision"`
	FundingAccount     string     `json:"funding_account_id"`
	RepaymentAccount   string     `json:"repayment_account_id,omitempty"`
	Merchant           string     `json:"merchant"`
	Category           string     `json:"category"`
	OriginalEvent      string     `json:"original_event_id,omitempty"`
	HistoricalOriginal bool       `json:"historical_original,omitempty"`
	AssetTitle         string     `json:"asset_title,omitempty"`
	Note               string     `json:"note,omitempty"`
}
type Event struct {
	ID          string   `json:"id"`
	Status      string   `json:"status"`
	Revision    int64    `json:"revision"`
	Facts       Facts    `json:"facts"`
	Reason      string   `json:"review_reason"`
	Posted      bool     `json:"posted"`
	EvidenceIDs []string `json:"evidence_ids"`
	CanonicalID string   `json:"canonical_id,omitempty"`
}
type Entry struct {
	AccountID string `json:"account_id"`
	Debit     Amount `json:"debit_minor"`
	Credit    Amount `json:"credit_minor"`
}
type Plan struct {
	Entries     []Entry   `json:"entries"`
	Purpose     string    `json:"purpose"`
	EffectiveAt time.Time `json:"effective_at"`
}
type Delivery struct {
	DeliveryID           string    `json:"delivery_id"`
	DeviceID             string    `json:"device_id"`
	SourceIdentity       string    `json:"source_identity"`
	SourceObjectKey      string    `json:"source_object_key"`
	SnapshotKey          string    `json:"snapshot_key"`
	CaptureSequence      int64     `json:"capture_sequence"`
	Package              string    `json:"package"`
	Title                string    `json:"title"`
	Text                 string    `json:"text"`
	BigText              string    `json:"big_text"`
	GroupSummary         bool      `json:"group_summary"`
	CaptureReason        string    `json:"capture_reason"`
	ObservedAt           time.Time `json:"observed_at"`
	NotificationPostedAt time.Time `json:"notification_posted_at"`
	Availability         string    `json:"content_availability"`
}
type Ack struct {
	DeliveryID    string `json:"delivery_id"`
	Status        string `json:"status"`
	ObservationID string `json:"observation_id,omitempty"`
	Reason        string `json:"reason,omitempty"`
}
type Journal struct {
	ID          string    `json:"id"`
	EventID     string    `json:"event_id"`
	Revision    int64     `json:"revision"`
	EffectiveAt time.Time `json:"effective_at"`
	ReversalOf  string    `json:"reversal_of,omitempty"`
	Entries     []Entry   `json:"entries"`
}

func IsUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	_, e := hex.DecodeString(s[:8] + s[9:13] + s[14:18] + s[19:23] + s[24:])
	return e == nil && s[8] == '-' && s[13] == '-' && s[18] == '-' && s[23] == '-'
}
func Hashable(v any) []byte {
	b, e := json.Marshal(v)
	if e != nil {
		panic(fmt.Sprintf("invalid internal JSON: %v", e))
	}
	return b
}
