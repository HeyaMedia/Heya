package vfs

import (
	"context"
	"fmt"
	"io/fs"
	"net"
	"time"

	smb2 "github.com/cloudsoda/go-smb2"
)

type smbConnection struct {
	conn    net.Conn
	session *smb2.Session
	share   *smb2.Share
}

func openSMB(rawURL string) (*Source, error) {
	cfg, err := ParseSMBURL(rawURL)
	if err != nil {
		return nil, err
	}

	addr := net.JoinHostPort(cfg.Host, cfg.Port)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var dialerNet net.Dialer
	conn, err := dialerNet.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", addr, err)
	}

	dialer := &smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{
			User:     cfg.Username,
			Password: cfg.Password,
		},
	}

	session, err := dialer.DialConn(ctx, conn, addr)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("SMB session to %s: %w", addr, err)
	}

	share, err := session.Mount(cfg.Share)
	if err != nil {
		session.Logoff()
		conn.Close()
		return nil, fmt.Errorf("mounting share %q on %s: %w", cfg.Share, addr, err)
	}

	var fsys fs.FS
	if cfg.Path != "" {
		fsys, err = fs.Sub(share.DirFS("."), cfg.Path)
		if err != nil {
			share.Umount()
			session.Logoff()
			conn.Close()
			return nil, fmt.Errorf("sub path %q: %w", cfg.Path, err)
		}
	} else {
		fsys = share.DirFS(".")
	}

	smbc := &smbConnection{conn: conn, session: session, share: share}

	return &Source{
		FS:       fsys,
		RootPath: RedactPath(rawURL),
		Close: func() error {
			smbc.share.Umount()
			smbc.session.Logoff()
			return smbc.conn.Close()
		},
	}, nil
}
