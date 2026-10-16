package db

import "time"

// EgressHost is the network filter gateway: one per container engine. It holds
// the last container and the hash of the token the gateway dials the CP with.
// There is a single row (ID "gateway"): a CP is pointed at one engine.
type EgressHost struct {
	ID          string `gorm:"primaryKey"`
	ContainerID string
	TokenHash   string `gorm:"index"`
	// Networks is the last health report's count of client networks the
	// gateway had an address on; 0 when it has never reported.
	Networks  int32
	HealthyAt time.Time
}

// EgressClient is one container on its own internal network, known by the
// gateway by the network a connection arrives on. BotID is set for a Bot box;
// ConnectorID for a STDIO sidecar. Subnet and Network are the CP's carving of
// the pool and are unique.
type EgressClient struct {
	ID          string `gorm:"primaryKey"`
	Kind        string // bot | stdio
	BotID       string `gorm:"index"`
	ConnectorID string `gorm:"index"`
	Name        string
	Subnet      string `gorm:"uniqueIndex"`
	Network     string `gorm:"uniqueIndex"`
	CreatedAt   time.Time
}

// EgressPattern is one entry of a Bot's own network list, laid over the
// operator's floor. A block entry always refuses, a trust entry silences
// flags, a pass entry splices TLS on SNI alone: none can loosen what the
// operator set (a threat list and a private address still win).
type EgressPattern struct {
	ID      string `gorm:"primaryKey"`
	BotID   string `gorm:"index:egress_pattern,unique"`
	Kind    string `gorm:"index:egress_pattern,unique"` // block | trust | pass
	Pattern string `gorm:"index:egress_pattern,unique"`
	// CreatedAt is what the list shows newest first.
	CreatedAt time.Time
}

// EgressFeedState is one threat list as the gateway last reported it, so the
// admin's Network page can show what each list holds and when it last loaded.
// The gateway owns the feed contents (and refreshes them itself); this only
// mirrors its word.
type EgressFeedState struct {
	ID string `gorm:"primaryKey"`
	// Entries is the count the gateway last loaded; 0 with no error means the
	// list is empty.
	Entries int32
	// Err is why the last download failed, empty when it was fine.
	Err string
	// DownloadedAt is the last good download; zero when never.
	DownloadedAt time.Time
	// ReportedAt is the last time the gateway mentioned the feed at all.
	ReportedAt time.Time
}

// EgressCA is the installation's certificate authority: the certificate and
// key stay with the CP and the gateway. One row; rotating replaces it.
type EgressCA struct {
	ID        string `gorm:"primaryKey"`
	CertPEM   []byte
	KeyPEM    []byte
	CreatedAt time.Time
}

// EgressTraffic is one minute of one client's traffic to one host, summed as
// the gateway reports it. Detail (what was asked for) is not kept: the audit
// timeline has that for the lines worth looking at. Trimmed to
// audit.traffic_days.
type EgressTraffic struct {
	ID          string `gorm:"primaryKey"`
	BotID       string `gorm:"index:egress_minute,unique;index:idx_egress_traffic_bot"`
	ConnectorID string `gorm:"index:egress_minute,unique"`
	Host        string `gorm:"index:egress_minute,unique"`
	// Minute is the start of the minute the traffic was in.
	Minute    time.Time `gorm:"index:egress_minute,unique;index:idx_egress_traffic_minute"`
	Requests  int32
	Blocked   int32
	Flagged   int32
	BytesUp   int64
	BytesDown int64
	UpdatedAt time.Time
}
