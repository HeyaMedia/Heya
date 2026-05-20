package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/karbowiak/heya/internal/worker"
)

type subtitleTrack struct {
	Index             int    `json:"index"`
	Language          string `json:"language"`
	Codec             string `json:"codec"`
	Title             string `json:"title"`
	IsDefault         bool   `json:"is_default"`
	IsForced          bool   `json:"is_forced"`
	IsHearingImpaired bool   `json:"is_hearing_impaired"`
}

func handleListSubtitles(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fileID, err := strconv.ParseInt(r.PathValue("file_id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid file id")
			return
		}

		q := sqlc.New(app.DB)
		file, err := q.GetLibraryFileByID(r.Context(), fileID)
		if err != nil {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}

		var info worker.MediaInfo
		if len(file.MediaInfo) > 0 {
			json.Unmarshal(file.MediaInfo, &info)
		}

		var tracks []subtitleTrack
		for _, s := range info.Streams {
			if s.CodecType != "subtitle" {
				continue
			}
			track := subtitleTrack{
				Index:    s.Index,
				Language: s.Tags["language"],
				Codec:    s.CodecName,
				Title:    s.Tags["title"],
			}
			if s.Disposition != nil {
				track.IsDefault = s.Disposition.Default == 1
				track.IsForced = s.Disposition.Forced == 1
				track.IsHearingImpaired = s.Disposition.HearingImpaired == 1
			}
			tracks = append(tracks, track)
		}

		writeJSON(w, http.StatusOK, tracks)
	}
}

func handleGetSubtitle(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fileID, err := strconv.ParseInt(r.PathValue("file_id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid file id")
			return
		}
		index, err := strconv.Atoi(r.PathValue("index"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid stream index")
			return
		}

		if app.TranscodeCache == nil {
			writeError(w, http.StatusServiceUnavailable, "transcoding not available")
			return
		}

		q := sqlc.New(app.DB)
		file, err := q.GetLibraryFileByID(r.Context(), fileID)
		if err != nil {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}

		var info worker.MediaInfo
		if len(file.MediaInfo) > 0 {
			json.Unmarshal(file.MediaInfo, &info)
		}

		subCodec := ""
		for _, s := range info.Streams {
			if s.CodecType == "subtitle" && s.Index == index {
				subCodec = s.CodecName
				break
			}
		}

		isASS := subCodec == "ass" || subCodec == "ssa"
		ext := ".vtt"
		outputCodec := "webvtt"
		contentType := "text/vtt; charset=utf-8"
		if isASS {
			ext = ".ass"
			outputCodec = "ass"
			contentType = "text/x-ssa; charset=utf-8"
		}

		cacheKey := fmt.Sprintf("sub_%d_%d", fileID, index)
		subPath := filepath.Join(app.TranscodeCache.SegmentDir(cacheKey), "subtitle"+ext)

		if _, err := os.Stat(subPath); err != nil {
			var extractErr error
			if vfs.IsSMBPath(file.Path) {
				extractErr = extractSubtitleSMB(r.Context(), file.Path, index, subPath, outputCodec)
			} else {
				extractErr = transcoder.ExtractSubtitlesAs(r.Context(), file.Path, index, subPath, outputCodec)
			}
			if extractErr != nil {
				writeError(w, http.StatusInternalServerError, "subtitle extraction failed: "+extractErr.Error())
				return
			}
		}

		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		http.ServeFile(w, r, subPath)
	}
}

func extractSubtitleSMB(ctx context.Context, smbPath string, streamIndex int, output string, codec string) error {
	lastSlash := strings.LastIndex(smbPath, "/")
	if lastSlash < 0 {
		return fmt.Errorf("invalid smb path: %s", smbPath)
	}

	source, err := vfs.Open(smbPath[:lastSlash])
	if err != nil {
		return fmt.Errorf("open smb dir: %w", err)
	}
	defer source.Close()

	f, err := source.FS.Open(smbPath[lastSlash+1:])
	if err != nil {
		return fmt.Errorf("open smb file: %w", err)
	}
	defer f.Close()

	return transcoder.ExtractSubtitlesFromReaderAs(ctx, f.(io.Reader), streamIndex, output, codec)
}
