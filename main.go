package main

import (
	"fmt"

	"context"
	"encoding/json"
	"github.com/fhs/gompd/mpd"
	"github.com/pressly/chi"
	"github.com/pressly/chi/render"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
	_ "expvar"
)

type (
	AlbumType struct {
		RFID string
		Name string
	}

	AlbumListType []AlbumType

	MPDInterpret string

	MPDSonglist map[MPDInterpret]MPDAlbums

	MPDAlbums map[string]MPDSongs

	MPDSongs []MPDSong

	MPDSong struct {
		Name     string
		Filename string
	}

	MPDClient struct {
		client *mpd.Client
	}
)

type UpdateSucceeded struct {
	updated bool
	mux     sync.Mutex
}

var updateSucceeded UpdateSucceeded

func (al AlbumListType) findAlbumInFileList(album string) (int, bool) {
	for key, entry := range al {
		if entry.Name == album {
			return key, true
		}
	}
	return 0, false
}

type MPDUpdateResult struct {
	//XMLName  xml.Name `xml:"MPD"`
	UpdateId int `json:"UpdateId"`
}

type MPDUpdateSuccessResult struct {
	//XMLName         xml.Name `xml:"MPD"`
	UpdateSucceeded bool `json:"UpdatedSucceeded"`
}

func updateRoutine() {
	mpdClient, err := NewMPDClient()
	if err != nil {
		//http.Error(w, http.StatusText(404), 404)
		return
	}
	defer mpdClient.client.Close()

	mpdUpdateSucceeded := false
	for !mpdUpdateSucceeded {
		status, _ := mpdClient.client.Status()
		fmt.Printf("Status: %v\n", status)
		//mpdUpdateResult := MPDUpdateSuccessResult{}
		_, okValue := status["updating_db"]
		fmt.Printf("okValue: %v\n", okValue)
		if !okValue {
			mpdUpdateSucceeded = true
		}
		time.Sleep(1000)
	}
	fileAlbumList := AlbumListType{}
	// Fill struct fileAlbum>List with album and rfids from json file
	file, err := ioutil.ReadFile("./albums.json")
	if err != nil {
		fmt.Printf("File error: %v\r\n", err)
	} else {
		err := json.Unmarshal(file, &fileAlbumList)
		fmt.Printf("UnmarshalErr: %v\n", err)
	}
	fmt.Printf("FileAlbumList: %v\r\n", fileAlbumList)

	// List all files, found in mpd database
	deleteAlbum := make(map[string]bool)
	for _, currentAlbum := range fileAlbumList {
		deleteAlbum[currentAlbum.Name] = true

	}

	mpdListAllInfo, _ := mpdClient.client.ListAllInfo("/")
	fmt.Printf("%v\n", mpdListAllInfo)
	fmt.Printf("Found <%v> files in mpd bibliothek\r\n", len(mpdListAllInfo))

	currentAlbum := ""
	lastAlbum := ""
	var mpdSongs MPDSongs
	mpdAlbums := make(MPDAlbums)
	for _, entry := range mpdListAllInfo {
		currentAlbum = entry["Album"]
		if lastAlbum != currentAlbum {
			if len(mpdSongs) > 0 {
				mpdAlbums[lastAlbum] = mpdSongs
				mpdSongs = []MPDSong{}
			}
			lastAlbum = currentAlbum
		}
		mpdSong := MPDSong{entry["Title"], entry["file"]}
		mpdSongs = append(mpdSongs, mpdSong)
	}
	if len(mpdSongs) > 0 {
		mpdAlbums[currentAlbum] = mpdSongs
		mpdSongs = []MPDSong{}
	}

	// Remove all Playlist from mpd
	playlists, _ := mpdClient.client.ListPlaylists()
	fmt.Printf("%v\n", playlists)
	for _, playlist := range playlists {
		mpdClient.client.PlaylistRemove(string(playlist["playlist"]))
	}

	fmt.Printf("DeleteAlbums before: %v\n", deleteAlbum)
	// Walk through all MPD albums and create playlist with all songs of album
	for currentAlbum, mpdSongs := range mpdAlbums {
		fmt.Printf("%s\n", currentAlbum)
		for _, mpdSong := range mpdSongs {
			mpdClient.client.PlaylistAdd(currentAlbum, mpdSong.Filename)
		}
		// Search for album in json filelist entries
		_, result := fileAlbumList.findAlbumInFileList(currentAlbum)
		// if not found, add it
		if !result {
			fmt.Printf("%s not found\n", currentAlbum)
			album := AlbumType{Name: currentAlbum}
			fileAlbumList = append(fileAlbumList, album)
		} else {
			// if found, remove it from albums to delete in the end
			fmt.Printf("Delete key %s from lsit\n", currentAlbum)
			delete(deleteAlbum, currentAlbum)

		}
	}

	// albums, which were in json file, but not in mpd database, are removed from mpd database
	// and has to be removed from json file
	fmt.Printf("DeleteAlbums: %v\n", deleteAlbum)
	for deleteAlbumKey, _ := range deleteAlbum {
		key, result := fileAlbumList.findAlbumInFileList(deleteAlbumKey)
		if result {
			fileAlbumList[key] = fileAlbumList[len(fileAlbumList)-1] // Replace it with the last one.
			fileAlbumList = fileAlbumList[:len(fileAlbumList)-1]     // Chop off the last one.
		}
	}
	fmt.Printf("fileAlbumList: %v\r\n", fileAlbumList)

	// Marshal struct and save new json file
	json, _ := json.MarshalIndent(fileAlbumList, "", "    ")
	ioutil.WriteFile("./albums.json", json, os.ModeAppend)
	updateSucceeded.mux.Lock()
	updateSucceeded.updated = true
	updateSucceeded.mux.Unlock()
}

func updateDB(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	mpdClient, ok := ctx.Value("mpdClient").(*MPDClient)
	if !ok {
		fmt.Print("Error on mpdClient")
		http.Error(w, http.StatusText(422), 422)
		return
	}
	status, _ := mpdClient.client.Update("/")
	mpdUpdateResult := MPDUpdateResult{}
	mpdUpdateResult.UpdateId = status
	fmt.Printf("Status: %v\n", status)
	updateSucceeded.mux.Lock()
	updateSucceeded.updated = false
	updateSucceeded.mux.Unlock()
	go updateRoutine()
	render.JSON(w, r, mpdUpdateResult)
}

func saveData(w http.ResponseWriter, r *http.Request) {
	albums := chi.URLParam(r, "albums")
	fmt.Printf("Albums: %v\n", albums)

	file, err := os.Create("./albums.json")
	//file, err := ioutil.WriteStr("./albums.json", albums)
	if err != nil {
		fmt.Printf("File error: %v\r\n", err)
		http.Error(w, http.StatusText(404), 404)
	} else {
		file.WriteString(albums)
		var jsonReturn struct {
			ok bool
		}
		jsonReturn.ok = true
		render.JSON(w, r, jsonReturn)
	}

}

func getUpdateStatus(w http.ResponseWriter, r *http.Request) {
	/*updateId := chi.URLParam(r, "updateStatus")
	ctx := r.Context()
	mpdClient, ok := ctx.Value("mpdClient").(*MPDClient)
	if !ok {
		fmt.Print("Error on mpdClient")
		http.Error(w, http.StatusText(422), 422)
		return
	}
	status, _ := mpdClient.client.Status()
	fmt.Printf("UpdateId: %v\n", updateId)
	fmt.Printf("Status: %v\n", status)
	mpdUpdateResult := MPDUpdateSuccessResult{}
	_, okValue := status["updating_db"]
	fmt.Printf("okValue: %v\n", okValue)
	if okValue {
		mpdUpdateResult.UpdateSucceeded = false
	} else {
		mpdUpdateResult.UpdateSucceeded = true
	}
	render.JSON(w, r, mpdUpdateResult)*/
	updateSucceeded.mux.Lock()
	updated := updateSucceeded.updated
	updateSucceeded.mux.Unlock()
	mpdUpdateResult := MPDUpdateSuccessResult{}
	fmt.Printf("updated: %v\n", updated)
	if updated {
		mpdUpdateResult.UpdateSucceeded = true
	} else {
		mpdUpdateResult.UpdateSucceeded = false
	}
	render.JSON(w, r, mpdUpdateResult)

}

func MPDContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mpdClient, err := NewMPDClient()
		if err != nil {
			http.Error(w, http.StatusText(404), 404)
			return
		}
		defer mpdClient.client.Close()
		ctx := context.WithValue(r.Context(), "mpdClient", mpdClient)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func NewMPDClient() (*MPDClient, error) {
	mpdClientConn, err := mpd.Dial("tcp", "localhost:6600")
	if err != nil {
		fmt.Printf("Error: %s\n", err)
		return nil, err
	}
	fmt.Print("Connected to mpd\n")
	mpdClient := MPDClient{}
	mpdClient.client = mpdClientConn
	return &mpdClient, nil
}

func getData(w http.ResponseWriter, r *http.Request) {

	// Fill struct fileAlbum>List with album and rfids from json file
	file, err := ioutil.ReadFile("./albums.json")
	if err != nil {
		fmt.Printf("File error: %v\r\n", err)
		http.Error(w, http.StatusText(404), 404)
		return
	} else {
		w.Write(file)
	}
}

func getSite(w http.ResponseWriter, r *http.Request) {
	dat, err := ioutil.ReadFile("index.html")
	if err != nil {
		http.Error(w, http.StatusText(404), 404)
		return
	}
	w.Write(dat)
}

func main() {
	r := chi.NewRouter()
	r.Route("/update", func(r chi.Router) {
		r.Use(MPDContext)
		r.Get("/", updateDB)
	})
	r.Route("/updateStatus/:updateStatus", func(r chi.Router) {
		r.Use(MPDContext)
		r.Get("/", getUpdateStatus)
	})
	r.Route("/data", func(r chi.Router) {
		r.Use(MPDContext)
		r.Get("/", getData)
	})
	r.Route("/saveData/:albums", func(r chi.Router) {
		r.Get("/", saveData)
	})
	r.Route("/", func(r chi.Router) {
		r.Get("/", getSite)
	})
	r.Handle("/debug/vars", http.DefaultServeMux)
	workDir, _ := os.Getwd()
	fmt.Printf("Working directory <%s>", workDir)
	filesDirJS := filepath.Join(workDir, "js")
	r.FileServer("/js", http.Dir(filesDirJS))
	filesDirCSS := filepath.Join(workDir, "css")
	r.FileServer("/css", http.Dir(filesDirCSS))
	filesDirFonts := filepath.Join(workDir, "fonts")
	r.FileServer("/fonts", http.Dir(filesDirFonts))

	http.ListenAndServe(":3000", r)

	/*fmt.Printf("Update mpd bibliothek\r\n")
	conn.Update("")
	time.Sleep(5 * time.Second)
	fmt.Printf("Updated mpd bibliothek\r\n")

	// List all files, found in mpd database
	mpdListAllInfo, err := conn.ListAllInfo("/")
	fmt.Printf("%v\n", mpdListAllInfo)
	fmt.Printf("Found <%v> files in mpd bibliothek\r\n", len(mpdListAllInfo))

	currentAlbum := ""
	lastAlbum := ""
	var mpdSongs MPDSongs
	mpdAlbums := make(MPDAlbums)
	for _, entry := range mpdListAllInfo {
		currentAlbum = entry["Album"]
		if lastAlbum != currentAlbum {
			if len(mpdSongs) > 0 {
				mpdAlbums[lastAlbum] = mpdSongs
				mpdSongs = []MPDSong{}
			}
			lastAlbum = currentAlbum
		}
		mpdSong := MPDSong{entry["Title"], entry["file"]}
		mpdSongs = append(mpdSongs, mpdSong)
	}
	if len(mpdSongs) > 0 {
		mpdAlbums[currentAlbum] = mpdSongs
		mpdSongs = []MPDSong{}
	}

	// Remove all Playlist from mpd
	playlists, err := conn.ListPlaylists()
	fmt.Printf("%v\n", playlists)
	for _, playlist := range playlists {
		conn.PlaylistRemove(string(playlist["playlist"]))
	}

	fmt.Printf("DeleteAlbums before: %v\n", deleteAlbum)
	// Walk through all MPD albums and create playlist with all songs of album
	for currentAlbum, mpdSongs := range mpdAlbums {
		fmt.Printf("%s\n", currentAlbum)
		for _, mpdSong := range mpdSongs {
			conn.PlaylistAdd(currentAlbum, mpdSong.Filename)
		}
		// Search for album in json filelist entries
		_, result := fileAlbumList.findAlbumInFileList(currentAlbum)
		// if not found, add it
		if !result {
			fmt.Printf("%s not found\n", currentAlbum)
			album := AlbumType{Name: currentAlbum}
			fileAlbumList = append(fileAlbumList, album)
		} else {
			// if found, remove it from albums to delete in the end
			fmt.Printf("Delete key %s from lsit\n", currentAlbum)
			delete(deleteAlbum, currentAlbum)

		}
	}

	// albums, which were in json file, but not in mpd database, are removed from mpd database
	// and has to be removed from json file
	fmt.Printf("DeleteAlbums: %v\n", deleteAlbum)
	for deleteAlbumKey, _ := range deleteAlbum {
		key, result := fileAlbumList.findAlbumInFileList(deleteAlbumKey)
		if result {
			fileAlbumList[key] = fileAlbumList[len(fileAlbumList)-1] // Replace it with the last one.
			fileAlbumList = fileAlbumList[:len(fileAlbumList)-1]     // Chop off the last one.
		}
	}
	fmt.Printf("fileAlbumList: %v\r\n", fileAlbumList)

	// Marshal struct and save new json file
	json, err := json.MarshalIndent(fileAlbumList, "", "    ")
	ioutil.WriteFile("./albums.json", json, os.ModeAppend)

	return

	line := ""
	line1 := ""
	// Loop printing the current status of MPD.
	for {
		status, err := conn.Status()
		if err != nil {
			log.Fatalln(err)
		}
		song, err := conn.CurrentSong()
		if err != nil {
			log.Fatalln(err)
		}
		if status["state"] == "play" {
			line1 = fmt.Sprintf("%s - %s", song["Artist"], song["Title"])
		} else {
			line1 = fmt.Sprintf("State: %s", status["state"])
		}
		if line != line1 {
			line = line1
			fmt.Println(line)
		}
		time.Sleep(1e9)
	}
	*/
}
