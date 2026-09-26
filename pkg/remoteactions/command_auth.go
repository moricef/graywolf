package remoteactions

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

const (
	RemoteCommandKeyID     = "A"
	remoteCommandDomain    = "LORA-APRS-RC"
	remoteCommandVersion   = byte(1)
	remoteCommandSecretLen = 32
	remoteCommandTagLen    = 12
)

var allowedRemoteCommands = map[string]struct{}{
	"EM=ON":  {},
	"EM=OFF": {},
	"TX=ON":  {},
	"TX=OFF": {},
	"COMMIT": {},
}

type RemoteCommandCredential struct {
	ID              uint   `gorm:"primaryKey"`
	Name            string `gorm:"size:64;not null"`
	TargetCall      string `gorm:"size:9;not null;uniqueIndex"`
	SecretBase64URL string `gorm:"column:secret_b64url;size:43;not null"`
	KeyID           string `gorm:"size:1;not null;default:'A'"`
	LastCounter     uint64 `gorm:"not null;default:0"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
	LastUsedAt      *time.Time
}

func (*RemoteCommandCredential) TableName() string { return "remote_command_credentials" }

func NormalizeRemoteCommandSecret(value string) (string, error) {
	clean := strings.TrimSpace(value)
	if clean == "" {
		return "", errors.New("secret required")
	}
	if strings.Contains(clean, "=") {
		return "", errors.New("secret must use unpadded Base64URL")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(clean)
	if err != nil || len(decoded) != remoteCommandSecretLen {
		return "", errors.New("secret must be 32 bytes encoded as 43 Base64URL characters without padding")
	}
	if base64.RawURLEncoding.EncodeToString(decoded) != clean {
		return "", errors.New("secret is not canonical Base64URL")
	}
	return clean, nil
}

func NormalizeRemoteCommand(command string) (string, error) {
	clean := strings.ToUpper(strings.TrimSpace(command))
	if _, ok := allowedRemoteCommands[clean]; !ok {
		return "", fmt.Errorf("unsupported remote command %q", command)
	}
	return clean, nil
}

func formatBase36(value uint64) string {
	const digits = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	if value == 0 {
		return "0"
	}
	var buf [13]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = digits[value%36]
		value /= 36
	}
	return string(buf[i:])
}

func appendLengthString(dst []byte, value string) ([]byte, error) {
	if len(value) > math.MaxUint16 {
		return nil, errors.New("authenticated field exceeds 65535 bytes")
	}
	var size [2]byte
	binary.BigEndian.PutUint16(size[:], uint16(len(value)))
	dst = append(dst, size[:]...)
	dst = append(dst, value...)
	return dst, nil
}

func BuildRemoteCommandEnvelope(secretBase64URL, controller, target string, counter uint64, command string) (string, error) {
	secretText, err := NormalizeRemoteCommandSecret(secretBase64URL)
	if err != nil {
		return "", err
	}
	secret, _ := base64.RawURLEncoding.DecodeString(secretText)
	target, err = NormalizeTargetCall(target)
	if err != nil {
		return "", err
	}
	controller = strings.ToUpper(strings.TrimSpace(controller))
	if controller == "" {
		return "", errors.New("controller callsign required")
	}
	if counter == 0 {
		return "", errors.New("counter must be non-zero")
	}
	command, err = NormalizeRemoteCommand(command)
	if err != nil {
		return "", err
	}

	authenticated := make([]byte, 0, 64)
	authenticated = append(authenticated, remoteCommandDomain...)
	authenticated = append(authenticated, remoteCommandVersion)
	authenticated, err = appendLengthString(authenticated, controller)
	if err != nil {
		return "", err
	}
	authenticated, err = appendLengthString(authenticated, target)
	if err != nil {
		return "", err
	}
	authenticated = append(authenticated, RemoteCommandKeyID...)
	var counterBytes [8]byte
	binary.BigEndian.PutUint64(counterBytes[:], counter)
	authenticated = append(authenticated, counterBytes[:]...)
	authenticated, err = appendLengthString(authenticated, command)
	if err != nil {
		return "", err
	}

	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(authenticated)
	tag := base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:remoteCommandTagLen])
	return fmt.Sprintf("!RC1:%s:%s:%s:%s", RemoteCommandKeyID, formatBase36(counter), command, tag), nil
}

type CommandCredentialStore struct {
	db        *gorm.DB
	reserveMu sync.Mutex
}

func NewCommandCredentialStore(db *gorm.DB) *CommandCredentialStore {
	return &CommandCredentialStore{db: db}
}

func (s *CommandCredentialStore) List(ctx context.Context) ([]RemoteCommandCredential, error) {
	var rows []RemoteCommandCredential
	err := s.db.WithContext(ctx).Order("target_call").Find(&rows).Error
	return rows, err
}

func (s *CommandCredentialStore) GetByTarget(ctx context.Context, target string) (*RemoteCommandCredential, error) {
	normalized, err := NormalizeTargetCall(target)
	if err != nil {
		return nil, err
	}
	var row RemoteCommandCredential
	if err := s.db.WithContext(ctx).Where("target_call = ?", normalized).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *CommandCredentialStore) Get(ctx context.Context, id uint) (*RemoteCommandCredential, error) {
	var row RemoteCommandCredential
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *CommandCredentialStore) Create(ctx context.Context, row *RemoteCommandCredential) error {
	if row == nil {
		return errors.New("remoteactions: nil command credential")
	}
	target, err := NormalizeTargetCall(row.TargetCall)
	if err != nil {
		return err
	}
	secret, err := NormalizeRemoteCommandSecret(row.SecretBase64URL)
	if err != nil {
		return err
	}
	row.TargetCall = target
	row.SecretBase64URL = secret
	row.KeyID = RemoteCommandKeyID
	row.LastCounter = 0
	return s.db.WithContext(ctx).Create(row).Error
}

func (s *CommandCredentialStore) Update(ctx context.Context, row *RemoteCommandCredential, replaceSecret bool) error {
	if row == nil || row.ID == 0 {
		return errors.New("remoteactions: nil command credential or zero id")
	}
	target, err := NormalizeTargetCall(row.TargetCall)
	if err != nil {
		return err
	}
	values := map[string]any{"name": row.Name, "target_call": target, "updated_at": time.Now().UTC()}
	if replaceSecret {
		secret, err := NormalizeRemoteCommandSecret(row.SecretBase64URL)
		if err != nil {
			return err
		}
		values["secret_b64url"] = secret
		values["last_counter"] = 0
		values["last_used_at"] = nil
	}
	result := s.db.WithContext(ctx).Model(&RemoteCommandCredential{}).Where("id = ?", row.ID).Updates(values)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (s *CommandCredentialStore) Delete(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&RemoteCommandCredential{}, id).Error
}

func (s *CommandCredentialStore) ReserveEnvelope(ctx context.Context, target, controller, command string) (string, uint64, error) {
	s.reserveMu.Lock()
	defer s.reserveMu.Unlock()
	target, err := NormalizeTargetCall(target)
	if err != nil {
		return "", 0, err
	}
	command, err = NormalizeRemoteCommand(command)
	if err != nil {
		return "", 0, err
	}
	var reserved RemoteCommandCredential
	now := time.Now().UTC()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&RemoteCommandCredential{}).
			Where("target_call = ? AND last_counter < ?", target, uint64(math.MaxInt64)).
			Updates(map[string]any{
				"last_counter": gorm.Expr("last_counter + 1"),
				"last_used_at": now,
				"updated_at":   now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			var count int64
			if err := tx.Model(&RemoteCommandCredential{}).Where("target_call = ?", target).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				return gorm.ErrRecordNotFound
			}
			return errors.New("remote command counter exhausted")
		}
		return tx.Where("target_call = ?", target).First(&reserved).Error
	})
	if err != nil {
		return "", 0, err
	}
	envelope, err := BuildRemoteCommandEnvelope(
		reserved.SecretBase64URL, controller, target, reserved.LastCounter, command,
	)
	if err != nil {
		return "", reserved.LastCounter, err
	}
	return envelope, reserved.LastCounter, nil
}
