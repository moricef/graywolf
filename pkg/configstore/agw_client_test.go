package configstore

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAGWClientUpgradeAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DB().Exec(`INSERT INTO kiss_interfaces (name,interface_type,mode,remote_host,remote_port) VALUES ('old','tcp-client','tnc','localhost',8001)`).Error; err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"tcp_protocol", "agw_port"} {
		if err = s.DB().Exec("ALTER TABLE kiss_interfaces DROP COLUMN " + column).Error; err != nil {
			t.Fatal(err)
		}
	}
	_ = s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, err := s.ListKissInterfaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].TCPProtocol != "" || rows[0].AgwPort != 0 || rows[0].RemotePort != 8001 {
		t.Fatalf("legacy KISS changed: %+v", rows)
	}
	row := rows[0]
	row.TCPProtocol = "agwpe"
	row.AgwPort = 1
	row.RemotePort = 8000
	if err = s.UpdateKissInterface(context.Background(), &row); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetKissInterface(context.Background(), row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TCPProtocol != "agwpe" || got.AgwPort != 1 {
		t.Fatalf("lost AGW settings: %+v", got)
	}
	row.Mode = KissModeModem
	if err = s.UpdateKissInterface(context.Background(), &row); err == nil {
		t.Fatal("store accepted AGW modem mode")
	}
}
