package main

import (
    "encoding/json"
    "fmt"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/ryjen/jukebox/internal/musicassistant"
)

func TestSignedCatalogChoiceBindsRoomSessionAndExpiry(t *testing.T) {
    s, clock := fixture(t)
    token := join(t,s,"living")
    other := join(t,s,"office")
    track := musicassistant.Track{URI:"filesystem://track/123",Name:"Dreams",Artist:"Fleetwood Mac",Provider:"filesystem"}
    choice, err := s.signCatalogChoice("living", token, track)
    if err != nil || choice.SelectionID == "" { t.Fatalf("issue selection: %v",err) }
    if _,err:=s.verifyCatalogChoice(choice.SelectionID,"living",token);err!=nil {t.Fatal(err)}
    if _,err:=s.verifyCatalogChoice(choice.SelectionID,"office",other);err==nil {t.Fatal("cross-room selection accepted")}
    if _,err:=s.verifyCatalogChoice(choice.SelectionID,"living",other);err==nil {t.Fatal("foreign session accepted")}
    forged:=choice.SelectionID[:len(choice.SelectionID)-1]+"!"
    if _,err:=s.verifyCatalogChoice(forged,"living",token);err==nil {t.Fatal("modified signature accepted")}
    *clock=clock.Add(11*60*1000000000)
    if _,err:=s.verifyCatalogChoice(choice.SelectionID,"living",token);err==nil {t.Fatal("expired selection accepted")}
}

func TestCatalogSelectionStoresSearchIdentityWithoutTrustingBrowserFields(t *testing.T) {
    upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
        var command struct{ Command string `json:"command"` }
        if err:=json.NewDecoder(r.Body).Decode(&command);err!=nil {t.Error(err)}
        if command.Command!="music/search" {t.Errorf("unexpected upstream command %s",command.Command)}
        w.Header().Set("Content-Type","application/json")
        fmt.Fprint(w,`{"tracks":[{"uri":"filesystem://track/123","name":"Dreams","provider":"filesystem","artists":[{"name":"Fleetwood Mac"}]}]}`)
    }))
    defer upstream.Close()
    s,_:=fixture(t)
    client,err:=musicassistant.New(upstream.URL,"service-readonly")
    if err!=nil{t.Fatal(err)}
    s.ma=client
    guest:=join(t,s,"living")
    other:=join(t,s,"office")
    rec:=call(s,"GET","/api/rooms/living/search?q=dreams",nil,guest)
    if rec.Code!=200 {t.Fatalf("search: %d %s",rec.Code,rec.Body.String())}
    var response struct{ Tracks []struct{ SelectionID string `json:"selectionId"`;Name string `json:"name"` } `json:"tracks"` }
    if err:=json.Unmarshal(rec.Body.Bytes(),&response);err!=nil||len(response.Tracks)!=1 {t.Fatalf("invalid search output: %v %s",err,rec.Body.String())}
    choice:=response.Tracks[0].SelectionID
    if choice==""||response.Tracks[0].Name!="Dreams"||strings.Contains(rec.Body.String(),`"uri":`){
        t.Fatalf("canonical URI incorrectly disclosed or choice absent: %s",rec.Body.String())
    }
    if got:=call(s,"POST","/api/rooms/living/requests",map[string]string{"selectionId":choice,"title":"Forged title"},guest).Code;got!=400 {t.Fatalf("metadata override accepted: %d",got)}
    if got:=call(s,"POST","/api/rooms/office/requests",map[string]string{"selectionId":choice},other).Code;got!=400 {t.Fatalf("cross-room selection accepted: %d",got)}
    if got:=call(s,"POST","/api/rooms/living/requests",map[string]string{"mediaUri":"filesystem://track/fake","title":"Fake","source":"cloud"},guest).Code;got!=400 {t.Fatalf("raw URI accepted: %d",got)}
    ok:=call(s,"POST","/api/rooms/living/requests",map[string]string{"selectionId":choice},guest)
    if ok.Code!=201 || !strings.Contains(ok.Body.String(),`"source":"catalog"`) {t.Fatalf("selection request failed: %d %s",ok.Code,ok.Body.String())}
    if strings.Contains(ok.Body.String(),"filesystem://track/123"){t.Fatal("raw URI leaked into guest request list")}
    s.mu.Lock()
    items:=s.requests["living"]
    s.mu.Unlock()
    if len(items)!=1||items[0].MediaURI!="filesystem://track/123"||items[0].Provider!="filesystem"||items[0].Title!="Dreams"{
        t.Fatalf("lost canonical media identity: %+v",items)
    }
    // Existing manual request intentions remain possible, but cannot pose as
    // canonical catalogue results.
    if got:=call(s,"POST","/api/rooms/living/requests",map[string]string{"title":"Suggestion","source":"local"},guest).Code;got!=201 {t.Fatalf("manual suggestions regressed: %d",got)}
    s.mu.Lock()
    manual:=s.requests["living"][1]
    s.mu.Unlock()
    if manual.MediaURI!=""||manual.Source!="local"{t.Fatalf("manual suggestion gained playback identity: %+v",manual)}
}
