package main

import (
 "encoding/json"
 "fmt"
 "net"
 "os"
 "os/exec"
 "runtime"
 "strings"
 "time"
 "github.com/mxschmitt/playwright-go"
)

func main() {
 pw,err:=playwright.Run(); must(err); defer pw.Stop()
 executable:=os.Getenv("DIAG_CHROME")
 if executable=="" {if runtime.GOOS=="windows" {executable=`C:\Program Files\Google\Chrome\Application\chrome.exe`} else {executable=pw.Chromium.ExecutablePath()}}
 profile:=os.Getenv("DIAG_PROFILE")
 if profile=="" {profile,err=os.MkdirTemp("","kf-ebay-diag-");must(err);defer os.RemoveAll(profile)}
 listener,err:=net.Listen("tcp","127.0.0.1:0");must(err);port:=listener.Addr().(*net.TCPAddr).Port;listener.Close()
 args:=[]string{"--user-data-dir="+profile,fmt.Sprintf("--remote-debugging-port=%d",port),"--no-first-run","--no-default-browser-check"}
 if runtime.GOOS=="linux"&&os.Geteuid()==0 {args=append(args,"--no-sandbox")}
 cmd:=exec.Command(executable,append(args,"about:blank")...);must(cmd.Start());defer cmd.Wait();defer cmd.Process.Kill()
 var b playwright.Browser
 for range 50 {b,err=pw.Chromium.ConnectOverCDP(fmt.Sprintf("http://127.0.0.1:%d",port),playwright.BrowserTypeConnectOverCDPOptions{Timeout:new(1000.0)});if err==nil {break};time.Sleep(200*time.Millisecond)}
 must(err); defer func(){c,e:=b.NewBrowserCDPSession();if e==nil {c.Send("Browser.close",nil)}}()
 p,err:=b.Contexts()[0].NewPage();must(err)
 fp,err:=p.Evaluate(`()=>({ua:navigator.userAgent,webdriver:navigator.webdriver,platform:navigator.platform,language:navigator.language,timezone:Intl.DateTimeFormat().resolvedOptions().timeZone})`);must(err);out("browser",fp)
 p.OnResponse(func(r playwright.Response){
  if r.Request().ResourceType()!="document" || !r.Request().IsNavigationRequest() {return}
  h:=r.Headers();safe:=map[string]string{}
  for _,k:=range []string{"server","via","content-type","x-cache","x-cache-status","x-mitmproxy-blocked-reason","x-squid-error","retry-after","location"} {if h[k]!="" {safe[k]=h[k]}}
  out("response",map[string]any{"url":r.URL(),"status":r.Status(),"headers":safe})
 })
 target:=os.Getenv("DIAG_URL");if target=="" {target="https://www.ebay.de/sch/i.html?_nkw=lego+technic&LH_BIN=1&_sop=10"}
 if os.Getenv("DIAG_HOME_FIRST")=="1" {_,err=p.Goto("https://www.ebay.de/",playwright.PageGotoOptions{WaitUntil:playwright.WaitUntilStateDomcontentloaded,Timeout:new(30000.0)});out("home_error",fmt.Sprint(err));time.Sleep(3*time.Second)}
 r,err:=p.Goto(target,playwright.PageGotoOptions{WaitUntil:playwright.WaitUntilStateDomcontentloaded,Timeout:new(30000.0)});out("navigation_error",fmt.Sprint(err));if err!=nil{return}
 if r.Status()<400 {_,err=p.WaitForSelector(".srp-results, .x-item-title__mainTitle",playwright.PageWaitForSelectorOptions{Timeout:new(20000.0)});out("ready_error",fmt.Sprint(err))}
 result,err:=p.Evaluate(`()=>({title:document.title,url:location.href,cards:document.querySelectorAll('.srp-results .s-card,.srp-results .s-item').length,body:document.querySelector('.srp-results')?'[listing body omitted]':document.body.innerText.slice(0,1500)})`);must(err);out("page",result)
 if details,err:=r.SecurityDetails();err==nil {out("tls",details)}
 if os.Getenv("DIAG_HOLD")!="" {time.Sleep(30*time.Second)}
}
func must(err error){if err!=nil{panic(err)}}
func out(label string,v any){b,_:=json.Marshal(v);fmt.Println(label,strings.TrimSpace(string(b)))}
