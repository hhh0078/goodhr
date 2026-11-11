// 文件作用说明：用 macOS 原生事件循环承接打开、重复打开和退出事件，Go 子进程继续负责业务与完整收尾。
#import <Cocoa/Cocoa.h>
#import <signal.h>

@interface GoodHRLauncher : NSObject <NSApplicationDelegate>
@property(strong) NSTask *agent;
@property(strong) NSStatusItem *statusItem;
@property BOOL quitting;
@end

@implementation GoodHRLauncher

// applicationDidFinishLaunching 创建原生菜单并启动同包中的 Go 程序，不复制业务或更新流程。
- (void)applicationDidFinishLaunching:(NSNotification *)notification {
    self.statusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
    self.statusItem.button.title = @"HR";
    self.statusItem.button.toolTip = @"GoodHR 本地程序";
    NSMenu *menu = [[NSMenu alloc] initWithTitle:@"GoodHR"];
    NSString *version = [[NSBundle mainBundle] objectForInfoDictionaryKey:@"CFBundleShortVersionString"] ?: @"";
    NSMenuItem *label = [menu addItemWithTitle:[@"GoodHR " stringByAppendingString:version] action:nil keyEquivalent:@""];
    label.enabled = NO;
    [menu addItemWithTitle:@"打开控制台" action:@selector(openConsole:) keyEquivalent:@""].target = self;
    [menu addItem:[NSMenuItem separatorItem]];
    [menu addItemWithTitle:@"退出本地程序" action:@selector(terminate:) keyEquivalent:@"q"].target = NSApp;
    self.statusItem.menu = menu;

    self.agent = [[NSTask alloc] init];
    self.agent.executableURL = [[[NSBundle mainBundle] executableURL].URLByDeletingLastPathComponent URLByAppendingPathComponent:@"goodhr-local-agent"];
    NSMutableArray *arguments = [NSMutableArray array];
    for (NSString *argument in [[NSProcessInfo processInfo].arguments subarrayWithRange:NSMakeRange(1, [NSProcessInfo processInfo].arguments.count - 1)]) {
        if (![argument hasPrefix:@"-psn_"]) [arguments addObject:argument];
    }
    self.agent.arguments = arguments;
    __weak GoodHRLauncher *weakSelf = self;
    self.agent.terminationHandler = ^(NSTask *task) {
        // AppKit 等待退出回执时仍处理主线程事件；避免退出中的主队列回调互相等待。
        [weakSelf performSelectorOnMainThread:@selector(agentDidExit:) withObject:task waitUntilDone:NO];
    };
    NSError *error = nil;
    if (![self.agent launchAndReturnError:&error]) {
        [self showError:@"启动文件暂时打不开，请重新安装后再试。"];
        [NSApp terminate:nil];
    }
}

// agentDidExit 在主线程确认 Go 收尾完成后回复系统，避免后台线程操作 AppKit 或提前结束启动器。
- (void)agentDidExit:(NSTask *)task {
    if (self.quitting) {
        [NSApp replyToApplicationShouldTerminate:YES];
    } else {
        if (task.terminationStatus != 0) [self showError:@"小助手没能正常启动，可以重新打开再试。"];
        [NSApp terminate:nil];
    }
}

// openConsole 只打开包内正式或测试域名的控制台，不再启动第二个 Go 进程。
- (void)openConsole:(id)sender {
    if (self.quitting) return;
    NSURL *infoURL = [self.agent.executableURL.URLByDeletingLastPathComponent URLByAppendingPathComponent:@"build-info.json"];
    NSDictionary *info = [NSJSONSerialization JSONObjectWithData:[NSData dataWithContentsOfURL:infoURL] ?: [NSData data] options:0 error:nil];
    NSString *base = [info[@"public_base_url"] isKindOfClass:[NSString class]] ? info[@"public_base_url"] : @"";
    NSURLComponents *url = [NSURLComponents componentsWithString:base];
    if (![url.scheme isEqualToString:@"https"] || !url.host.length) {
        [self showError:@"控制台地址没准备好，请重新安装后再试。"];
        return;
    }
    url.path = @"/admin";
    url.queryItems = @[[NSURLQueryItem queryItemWithName:@"local_port" value:@"43129"]];
    [[NSWorkspace sharedWorkspace] openURL:url.URL];
}

// applicationShouldHandleReopen 立即响应 Finder 重复打开事件，避免 Go 后台服务被判定为没有响应。
- (BOOL)applicationShouldHandleReopen:(NSApplication *)sender hasVisibleWindows:(BOOL)flag {
    [self openConsole:nil];
    return NO;
}

// applicationShouldTerminate 只向本启动器创建的 Go 子进程发送正常退出信号，并等待任务、浏览器和数据收尾。
- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)sender {
    if (!self.agent.running) return NSTerminateNow;
    if (!self.quitting) {
        self.quitting = YES;
        self.statusItem.button.title = @"退出中…";
        self.statusItem.button.toolTip = @"正在停止任务、关闭浏览器并保存状态";
        [self.agent terminate];
    }
    return NSTerminateLater;
}

// showError 用原生提示给出可执行的下一步，不向磁盘写入新的日志文件。
- (void)showError:(NSString *)message {
    NSAlert *alert = [[NSAlert alloc] init];
    alert.messageText = @"我小声提醒一下";
    alert.informativeText = message;
    [alert addButtonWithTitle:@"知道了"];
    [alert runModal];
}
@end

// main 在主线程运行 AppKit；Windows 不编译本文件，两个 Mac 架构使用相同启动和退出逻辑。
int main(int argc, const char *argv[]) {
    @autoreleasepool {
        NSApplication *app = [NSApplication sharedApplication];
        [app setActivationPolicy:NSApplicationActivationPolicyAccessory];
        static GoodHRLauncher *delegate;
        delegate = [[GoodHRLauncher alloc] init];
        app.delegate = delegate;
        // 安装、终端或系统请求退出时也转入同一正常退出流程，不遗留业务子进程。
        signal(SIGTERM, SIG_IGN);
        static dispatch_source_t termination;
        termination = dispatch_source_create(DISPATCH_SOURCE_TYPE_SIGNAL, SIGTERM, 0, dispatch_get_main_queue());
        dispatch_source_set_event_handler(termination, ^{
            [NSApp performSelectorOnMainThread:@selector(terminate:) withObject:nil waitUntilDone:NO];
        });
        dispatch_resume(termination);
        [app run];
    }
    return 0;
}
