// 文件作用说明：验证浏览器原生观察模块的进度、取消、目录确认和退出落盘，避免接管下载或丢失终态。
import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import vm from "node:vm";
import test from "node:test";

/** observerFixture 创建浏览器系统模块替身，测试仅在 Node 沙箱执行，不操作任何网页。 */
async function observerFixture() {
 const directory = os.tmpdir();
 const values = new Map([["goodhr.downloads.settings",path.join(directory,"settings.json")],["goodhr.downloads.snapshot",path.join(directory,"snapshot.json")],["goodhr.downloads.session","session"]]);
 const state = { settings:{version:1,session_id:"session",revision:0,directory}, snapshots:[], timers:new Map(), errors:[], view:null, shutdown:null, interval:null };
 const modules = {
  Downloads:{ALL:"all",async getList(){return {async addView(view){state.view=view;}};}},
  setInterval(callback){state.interval=callback;return 1;},clearInterval(){state.interval=null;},
  setTimeout(callback){const key=Symbol();state.timers.set(key,callback);return key;},clearTimeout(key){state.timers.delete(key);},
  AsyncShutdown:{profileBeforeChange:{addBlocker(name,callback){state.shutdown=callback;}}},
 };
 const source=await fs.readFile(new URL("../../internal/runtime/native_downloads.js",import.meta.url),"utf8");
 const context = vm.createContext({
  ChromeUtils:{importESModule(){return modules;}},
  Services:{prefs:{getStringPref(name,fallback){return values.get(name)??fallback;},setStringPref(name,value){values.set(name,value);},setIntPref(name,value){values.set(name,value);},setBoolPref(name,value){values.set(name,value);}}},
  IOUtils:{async readJSON(){return state.settings;},async writeJSON(target,snapshot,options){assert.equal(options.tmpPath,`${target}.tmp`);state.snapshots.push(JSON.parse(JSON.stringify(snapshot)));}},
  PathUtils:{isAbsolute:path.isAbsolute},Cu:{reportError(error){state.errors.push(error);}},
 });
 vm.runInContext(source.replace("export async function startGoodHRNativeDownloads", "async function startGoodHRNativeDownloads"),context);
 await context.startGoodHRNativeDownloads();
 return {state,values};
}

/** flush 执行合并的进度快照，再等待写入队列完成。 */
async function flush(state) {
 const callbacks=[...state.timers.values()];state.timers.clear();
 for(const callback of callbacks)callback();
 await new Promise(resolve=>setImmediate(resolve));
}

/** 验证观察器只记录浏览器真实路径，完成和取消进入对应终态。 */
test("原生观察器记录进度、完成、取消并在退出落盘",async()=>{
 const {state}=await observerFixture();
 const download={target:{path:path.join(os.tmpdir(),"简历.pdf"),size:606},source:{url:"https://example.test/resume.pdf",referrerInfo:{originalReferrer:{spec:"https://example.test/"}}},startTime:new Date("2026-09-18T08:00:00Z"),currentBytes:12,succeeded:false,canceled:false,error:null};
 state.view.onDownloadAdded(download);await flush(state);
 assert.equal(state.snapshots.at(-1).downloads[0].status,"pending");
 download.succeeded=true;state.view.onDownloadChanged(download);await flush(state);
 assert.equal(state.snapshots.at(-1).downloads[0].status,"saved");
 assert.equal(state.snapshots.at(-1).downloads[0].file_path,download.target.path);
 assert.equal(state.snapshots.at(-1).downloads[0].size,606);
 const canceled={...download,target:{...download.target,path:path.join(os.tmpdir(),"cancel.pdf")},succeeded:false,canceled:true};
 state.view.onDownloadAdded(canceled);
 await state.shutdown();
 assert.equal(state.snapshots.at(-1).downloads.find(item=>item.file_path===canceled.target.path).status,"failed");
 assert.equal(state.interval,null);
 assert.equal(state.errors.length,0);
});

/** 验证目录设置必须属于当前会话，确认版本只在首选项成功更新后回写。 */
test("原生目录确认校验会话且保留下载历史",async()=>{
 const {state,values}=await observerFixture();
 const next=path.join(os.tmpdir(),"下一次下载");
 state.settings={...state.settings,directory:next,revision:1};
 await state.interval();await new Promise(resolve=>setImmediate(resolve));
 assert.equal(values.get("browser.download.dir"),next);
 assert.equal(state.snapshots.at(-1).revision,1);
 state.settings={...state.settings,session_id:"other",directory:path.join(os.tmpdir(),"other"),revision:2};
 await state.interval();await new Promise(resolve=>setImmediate(resolve));
 assert.equal(values.get("browser.download.dir"),next);
 assert.equal(state.snapshots.at(-1).revision,1);
 assert.equal(state.errors.length,1);
 await state.shutdown();
});
