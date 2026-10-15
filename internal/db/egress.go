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

// EgressCA is the installation's certificate authority: the certificate and
// key stay with the CP and the gateway. One row; rotating replaces it.
type EgressCA struct {
	ID        string `gorm:"primaryKey"`
	CertPEM   []byte
	KeyPEM    []byte
	CreatedAt time.Time
}
