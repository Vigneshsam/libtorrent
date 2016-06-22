package libtorrent

// #include <stdlib.h>
import "C"

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"path"
	"sync"
)

//export CreateTorrentFile
func CreateTorrentFile(path string) []byte {
	mi := metainfo.MetaInfo{}
	// for _, a := range announs {
	//   mi.AnnounceList = append(mi.AnnounceList, []string{a})
	// }
	mi.SetDefaults()
	err = mi.Info.BuildFromFilePath(path)
	if err != nil {
		return nil
	}
	var b bytes.Buffer
	w := bufio.NewWriter(&b)
	err = mi.Write(w)
	if err != nil {
		return nil
	}
	err = w.Flush()
	if err != nil {
		return nil
	}
	return b.Bytes()
}

type torrentOpener struct {
}

func (m *torrentOpener) OpenTorrent(info *metainfo.InfoEx) (storage.Torrent, error) {
	var p string

	if s, ok := filestorage[info.Hash()]; !ok {
		p = clientConfig.DataDir
	} else {
		p = s.Path
	}

	return storage.NewFile(p).OpenTorrent(info)
}

// Create
//
// Create libtorrent object
//
//export Create
func Create() bool {
	torrents = make(map[int]*torrent.Torrent)
	filestorage = make(map[metainfo.Hash]*fileStorage)
	index = 0

	clientConfig.DefaultStorage = &torrentOpener{}
	clientConfig.Seed = true

	client, err = torrent.NewClient(&clientConfig)
	if err != nil {
		return false
	}

	return true
}

type BytesInfo struct {
	Downloaded int64
	Uploaded   int64
}

func Stats() *BytesInfo {
	d, u := client.Stats()
	return &BytesInfo{d, u}
}

// Get Torrent Count
//
//export Count
func Count() int {
	return len(torrents)
}

var (
	builtinAnnounceList = [][]string{
		{"udp://tracker.openbittorrent.com:80"},
		{"udp://tracker.kicks-ass.net:80/announce"},
	}
)

//export CreateTorrent
func CreateTorrent(p string) int {
	var t *torrent.Torrent

	mi := &metainfo.MetaInfo{
		AnnounceList: builtinAnnounceList,
	}

	mi.SetDefaults()

	err = mi.Info.BuildFromFilePath(p)
	if err != nil {
		return -1
	}

	mi.Info.UpdateBytes()

	if _, ok := filestorage[mi.Info.Hash()]; ok {
		err = errors.New("Already exists")
		return -1
	}

	filestorage[mi.Info.Hash()] = &fileStorage{Path: path.Dir(p)}

	t, err = client.AddTorrent(mi)
	if err != nil {
		return -1
	}

	return register(t)
}

// AddMagnet
//
// Add magnet link to download list
//
//export AddMagnet
func AddMagnet(path string, magnet string) int {
	var t *torrent.Torrent
	var spec *torrent.TorrentSpec

	spec, err = torrent.TorrentSpecFromMagnetURI(magnet)
	if err != nil {
		return -1
	}

	if _, ok := filestorage[spec.InfoHash]; ok {
		err = errors.New("Already exists")
		return -1
	}

	filestorage[spec.InfoHash] = &fileStorage{Path: path}

	t, _, err = client.AddTorrentSpec(spec)
	if err != nil {
		return -1
	}

	return register(t)
}

// AddTorrent
//
// Add torrent to download list
//
//export AddTorrent
func AddTorrent(path string, file string) int {
	var t *torrent.Torrent
	var metaInfo *metainfo.MetaInfo

	metaInfo, err = metainfo.LoadFromFile(file)
	if err != nil {
		return -1
	}

	if _, ok := filestorage[metaInfo.Info.Hash()]; ok {
		err = errors.New("Already exists")
		return -1
	}

	filestorage[metaInfo.Info.Hash()] = &fileStorage{Path: path}

	t, err = client.AddTorrent(metaInfo)
	if err != nil {
		return -1
	}

	return register(t)
}

// Get Torrent file from runtime torrent
//
//export GetTorrent
func GetTorrent(i int) []byte {
	t := torrents[i]

	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	err = t.Metainfo().Write(w)
	if err != nil {
		return nil
	}
	err = w.Flush()
	if err != nil {
		return nil
	}
	return buf.Bytes()
}

// SaveTorrent
//
// Every torrent application restarts it require to check files consistency. To
// avoid this, and save machine time we need to store torrents runtime states
// completed pieces and other information externaly.
//
// Save runtime torrent data to state file
//
//export SaveTorrent
func SaveTorrent(i int) []byte {
	t := torrents[i]

	var buf []byte

	buf, err = client.SaveTorrent(t)
	if err != nil {
		return nil
	}

	return buf
}

// LoadTorrent
//
// Load runtime torrent data from saved state file
//
//export LoadTorrent
func LoadTorrent(path string, buf []byte) int {
	var t *torrent.Torrent

	// will be read immidialtly within client.LoadTorrent call
	clientConfig.DataDir = path

	t, err = client.LoadTorrent(buf)
	if err != nil {
		return -1
	}

	// prevent addind magnets/torrents with same hash
	filestorage[t.InfoHash()] = &fileStorage{Path: path}

	return register(t)
}

// Separate load / create torrent from network activity.
//
// Start announce torrent, seed/download
//
//export StartTorrent
func StartTorrent(i int) {
	t := torrents[i]

	client.StartTorrent(t)

	go func() {
		<-t.GotInfo()
		t.DownloadAll()
	}()
}

// Stop torrent from announce, check, seed, download
//
//export StopTorrent
func StopTorrent(i int) {
	t := torrents[i]
	if client.ActiveTorrent(t) {
		t.Drop()
	}
}

// CheckTorrent
//
// Check torrent file consisteny (pices hases) on a disk. Pause torrent if
// downloading, resume after.
//
//export CheckTorrent
func CheckTorrent(i int) {
	t := torrents[i]
	client.CheckTorrent(t)
}

// Remote torrent for library
//
//export RemoveTorrent
func RemoveTorrent(i int) {
	t := torrents[i]
	if client.ActiveTorrent(t) {
		t.Drop()
	}
	unregister(i)
}

//export Error
func Error() string {
	if err != nil {
		return err.Error()
	}
	return ""
}

//export Close
func Close() {
	if client != nil {
		client.Close()
		client = nil
	}
}

//
// Torrent* methods
//

// Get Magnet from runtime torrent.
//
//export TorrentMagnet
func TorrentMagnet(i int) string {
	t := torrents[i]
	return t.Metainfo().Magnet().String()
}

func TorrentMetainfo(i int) *metainfo.MetaInfo {
	t := torrents[i]
	return t.Metainfo()
}

//export TorrentHash
func TorrentHash(i int) string {
	t := torrents[i]
	h := t.InfoHash()
	return h.HexString()
}

//export TorrentName
func TorrentName(i int) string {
	t := torrents[i]
	return t.Name()
}

//export TorrentActive
func TorrentActive(i int) bool {
	t := torrents[i]
	return client.ActiveTorrent(t)
}

const (
	StatusPaused      int32 = 0
	StatusDownloading int32 = 1
	StatusSeeding     int32 = 2
	StatusQueued      int32 = 3
)

//export TorrentStatus
func TorrentStatus(i int) int32 {
	t := torrents[i]

	if client.ActiveTorrent(t) {
		if t.Info() != nil {
			// TODO t.Seeding() not working
			if t.BytesCompleted() == t.Length() {
				if t.Seeding() {
					return StatusSeeding
				}
			}
		}
		return StatusDownloading
	} else {
		return StatusPaused
	}
}

//export TorrentBytesLength
func TorrentBytesLength(i int) int64 {
	t := torrents[i]
	return t.Length()
}

//export TorrentBytesCompleted
func TorrentBytesCompleted(i int) int64 {
	t := torrents[i]
	return t.BytesCompleted()
}

func TorrentStats(i int) *BytesInfo {
	t := torrents[i]
	d, u := t.Stats()
	return &BytesInfo{d, u}
}

type File struct {
	Check  bool
	Path   string
	Length int64
	//BytesCompleted int64
}

func TorrentFilesCount(i int) int {
	t := torrents[i]
	f := filestorage[t.InfoHash()]
	f.Files = nil
	for _, v := range t.Files() {
		p := File{}
		p.Check = true
		p.Path = v.Path()
		p.Length = v.Length()
		f.Files = append(f.Files, p)
	}
	return len(f.Files)
}

// return torrent files array
func TorrentFiles(i int, p int) *File {
	t := torrents[i]
	f := filestorage[t.InfoHash()]
	return &f.Files[p]
}

type Peer struct {
	Name   string
	Addr   string
	Source string
	// Peer is known to support encryption.
	SupportsEncryption bool
	// how many data we downloaded/uploaded from peer
	Downloaded int64
	Uploaded   int64
}

const (
	peerSourceTracker  = '\x00' // It's the default.
	peerSourceIncoming = 'I'
	peerSourceDHT      = 'H'
	peerSourcePEX      = 'X'
)

func TorrentPeersCount(i int) int {
	t := torrents[i]
	f := filestorage[t.InfoHash()]

	f.Peers = nil

	for _, v := range t.Peers() {
		var p string
		switch v.Source {
		case peerSourceTracker:
			p = "Tracker"
		case peerSourceIncoming:
			p = "Incoming"
		case peerSourceDHT:
			p = "DHT"
		case peerSourcePEX:
			p = "PEX"
		}
		f.Peers = append(f.Peers, Peer{string(v.Id[:]), fmt.Sprintf("%s:%d", v.IP.String(), v.Port), p, v.SupportsEncryption, v.Downloaded, v.Uploaded})
	}

	return len(f.Peers) // t.PeersCount()
}

func TorrentPeers(i int, p int) *Peer {
	t := torrents[i]
	f := filestorage[t.InfoHash()]
	return &f.Peers[p]
}

func TorrentPiecesLength(i int) int64 {
	t := torrents[i]
	return t.Info().PieceLength
}

func TorrentPiecesCount(i int) int {
	t := torrents[i]
	f := filestorage[t.InfoHash()]
	f.Pieces = t.PieceStateRuns()
	return len(f.Pieces) //t.NumPieces()
}

func TorrentPieces(i int, p int) *torrent.PieceStateRun {
	t := torrents[i]
	f := filestorage[t.InfoHash()]
	return &f.Pieces[p]
}

//export TorrentCreator
func TorrentCreator(i int) string {
	t := torrents[i]
	return t.Metainfo().CreatedBy
}

//export TorrentCreateOn
func TorrentCreateOn(i int) int64 {
	t := torrents[i]
	return t.Metainfo().CreationDate
}

//export TorrentComment
func TorrentComment(i int) string {
	t := torrents[i]
	return t.Metainfo().Comment
}

func TorrentDateAdded(i int) int64 {
	t := torrents[i]
	a, _ := t.Dates()
	return a
}

func TorrentDateCompleted(i int) int64 {
	t := torrents[i]
	_, c := t.Dates()
	return c
}

// TorrentFileRename
//
// To implement this we need to keep two Metainfo one for network operations,
// and second for local file storage.
//
//export TorrentFileRename
func TorrentFileRename(i int, f int, n string) {
	panic("not implement")
}

type Tracker struct {
	// Tracker URI or DHT, LSD, PE
	Addr         string
	Error        string
	LastAnnounce int64
	NextAnnounce int64
	Peers        int

	// scrape info
	LastScrape int64
	Seeders    int
	Leechers   int
	Downloaded int
}

func TorrentTrackersCount(i int) int {
	t := torrents[i]
	f := filestorage[t.InfoHash()]
	f.Trackers = nil
	for _, v := range t.Trackers() {
		f.Trackers = append(f.Trackers, Tracker{v.Url, v.Err, v.LastAnnounce, v.NextAnnounce, v.Peers, 0, 0, 0, 0})
	}
	return len(f.Trackers)
}

func TorrentTrackers(i int, p int) *Tracker {
	t := torrents[i]
	f := filestorage[t.InfoHash()]
	return &f.Trackers[p]
}

//
// protected
//

type fileStorage struct {
	Path     string
	Trackers []Tracker
	Pieces   []torrent.PieceStateRun
	Files    []File
	Peers    []Peer
}

var clientConfig torrent.Config
var client *torrent.Client
var err error
var torrents map[int]*torrent.Torrent
var filestorage map[metainfo.Hash]*fileStorage
var index int
var mu sync.Mutex

func register(t *torrent.Torrent) int {
	mu.Lock()
	defer mu.Unlock()

	index++
	for torrents[index] != nil {
		index++
	}
	torrents[index] = t

	return index
}

func unregister(i int) {
	mu.Lock()
	defer mu.Unlock()

	t := torrents[i]

	delete(filestorage, t.InfoHash())

	delete(torrents, i)
}
