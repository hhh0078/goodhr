# 文件作用：只覆盖已验证隔离 Camoufox 的可见窗口，使用不激活、不拦截鼠标的独立 Windows 浮动窗；不截屏、不读取标题或网页。
param([int]$BrowserPid = 0, [ValidateRange(1, 300)][int]$Seconds = 60, [switch]$SelfTest)
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)

# Assert-IsolatedBrowser 再次核对指定 PID 的实际路径，拒绝用户正式 GoodHR 组件。
function Assert-IsolatedBrowser([int]$TargetPid) {
    if ($TargetPid -le 0) { throw 'BrowserPid 必须是正整数' }
    $row = Get-CimInstance -ClassName Win32_Process -Filter ('ProcessId = ' + $TargetPid)
    $binary = [string]$row.ExecutablePath
    $normalized = $binary.Replace('\', '/').ToLowerInvariant()
    if (-not $normalized.Contains('/camoufox-upgrade/') -or -not $normalized.EndsWith('/camoufox.exe') -or
        $normalized.Contains('/appdata/local/goodhr/') -or $normalized.Contains('/appdata/roaming/goodhr/')) {
        throw '目标不是 camoufox-upgrade 内的隔离 Camoufox，未创建遮挡窗'
    }
}

$source = @'
using System;
using System.Diagnostics;
using System.Drawing;
using System.Runtime.InteropServices;
using System.Windows.Forms;

// GoodHRWindowsCover 仅通过指定 PID 的公开窗口元数据创建真实遮挡，不访问窗口标题或屏幕内容。
public static class GoodHRWindowsCover {
    [StructLayout(LayoutKind.Sequential)]
    public struct Rect { public int Left, Top, Right, Bottom; }
    private delegate bool WindowVisitor(IntPtr handle, IntPtr parameter);
    [DllImport("user32.dll")] private static extern bool EnumWindows(WindowVisitor callback, IntPtr parameter);
    [DllImport("user32.dll")] private static extern uint GetWindowThreadProcessId(IntPtr handle, out uint process);
    [DllImport("user32.dll")] private static extern bool GetWindowRect(IntPtr handle, out Rect rectangle);
    [DllImport("user32.dll")] private static extern bool IsWindowVisible(IntPtr handle);
    [DllImport("user32.dll")] private static extern bool IsIconic(IntPtr handle);
    [DllImport("user32.dll")] private static extern bool SetWindowPos(IntPtr handle, IntPtr order, int x, int y, int width, int height, uint flags);
    [DllImport("user32.dll")] private static extern bool SetLayeredWindowAttributes(IntPtr handle, uint color, byte alpha, uint flags);
    [DllImport("user32.dll")] private static extern bool SetProcessDpiAwarenessContext(IntPtr context);
    [DllImport("user32.dll")] private static extern bool SetProcessDPIAware();

    // Area 只接受有足够可见面积的窗口，避免把最小化坐标或辅助窗口当成浏览器正文。
    private static long Area(Rect rectangle) {
        int width = rectangle.Right - rectangle.Left, height = rectangle.Bottom - rectangle.Top;
        return width > 100 && height > 100 ? (long)width * height : 0;
    }

    // LargestWindow 只枚举指定浏览器 PID 的可见窗口，选择最大边界而不读取标题。
    private static Rect LargestWindow(int browserPid) {
        Rect selected = new Rect();
        EnumWindows(delegate(IntPtr handle, IntPtr parameter) {
            uint owner;
            GetWindowThreadProcessId(handle, out owner);
            Rect rectangle;
            if (owner == (uint)browserPid && IsWindowVisible(handle) && !IsIconic(handle) && GetWindowRect(handle, out rectangle) && Area(rectangle) > Area(selected)) selected = rectangle;
            return true;
        }, IntPtr.Zero);
        if (Area(selected) == 0) throw new InvalidOperationException("指定测试浏览器没有可见主窗口");
        return selected;
    }

    // CoverForm 始终不激活应用，并以分层窗口和透明命中测试让真实输入继续交给被遮挡浏览器。
    private sealed class CoverForm : Form {
        // ShowWithoutActivation 禁止显示夹具时抢占前台焦点。
        protected override bool ShowWithoutActivation { get { return true; } }
        // CreateParams 设置无激活、工具窗、分层和鼠标穿透标志，不改变浏览器窗口。
        protected override CreateParams CreateParams {
            get { CreateParams value = base.CreateParams; value.ExStyle |= 0x08000000 | 0x00000080 | 0x00080000 | 0x00000020; return value; }
        }
        // WndProc 不接受鼠标命中或鼠标激活，维持自然遮挡而不是暂停浏览器。
        protected override void WndProc(ref Message message) {
            if (message.Msg == 0x0084) { message.Result = new IntPtr(-1); return; }
            if (message.Msg == 0x0021) { message.Result = new IntPtr(3); return; }
            base.WndProc(ref message);
        }
    }

    // SelfTest 仅检查纯边界计算，不枚举窗口、不创建应用窗体。
    public static bool SelfTest() {
        return Area(new Rect { Left = -100, Top = 20, Right = 900, Bottom = 720 }) == 700000 &&
               Area(new Rect { Left = 0, Top = 0, Right = 50, Bottom = 30 }) == 0;
    }

    // Run 根据目标可见窗口的真实坐标创建自然色遮挡窗，确认自身窗口边界后输出 ready 并自动关闭。
    public static void Run(int browserPid, int seconds) {
        try { SetProcessDpiAwarenessContext(new IntPtr(-4)); } catch (EntryPointNotFoundException) { SetProcessDPIAware(); }
        Rect target = LargestWindow(browserPid);
        Application.EnableVisualStyles();
        Application.SetCompatibleTextRenderingDefault(false);
        using (CoverForm cover = new CoverForm()) {
            cover.FormBorderStyle = FormBorderStyle.None;
            cover.StartPosition = FormStartPosition.Manual;
            cover.AutoScaleMode = AutoScaleMode.None;
            cover.Bounds = Rectangle.FromLTRB(target.Left, target.Top, target.Right, target.Bottom);
            cover.TopMost = true;
            cover.ShowInTaskbar = false;
            cover.BackColor = Color.FromArgb(245, 240, 222);
            Label label = new Label { Dock = DockStyle.Fill, TextAlign = ContentAlignment.MiddleCenter,
                Text = "GoodHR 遮挡测试，" + seconds + " 秒后关闭", ForeColor = Color.FromArgb(60, 60, 56),
                Font = new Font(SystemFonts.DefaultFont.FontFamily, 16, FontStyle.Regular) };
            cover.Controls.Add(label);
            using (Timer readiness = new Timer())
            using (Timer expiry = new Timer()) {
                readiness.Interval = 400;
                expiry.Interval = seconds * 1000;
                readiness.Tick += delegate {
                    readiness.Stop();
                    Rect actual = new Rect();
                    bool visible = IsWindowVisible(cover.Handle) && GetWindowRect(cover.Handle, out actual);
                    if (!visible || actual.Left != target.Left || actual.Top != target.Top || actual.Right != target.Right || actual.Bottom != target.Bottom) {
                        Console.WriteLine("{\"ready\":false,\"error_message\":\"遮挡窗没有确认实际显示在目标边界\"}");
                        Console.Out.Flush(); cover.Close(); return;
                    }
                    Console.WriteLine("{\"ready\":true,\"covered\":true,\"cover_pid\":" + Process.GetCurrentProcess().Id +
                        ",\"browser_pid\":" + browserPid + ",\"seconds\":" + seconds + ",\"window_visible\":true,\"ignores_mouse_events\":true,\"activates_application\":false,\"target_bounds\":{\"x\":" +
                        target.Left + ",\"y\":" + target.Top + ",\"width\":" + (target.Right - target.Left) + ",\"height\":" + (target.Bottom - target.Top) + "}}");
                    Console.Out.Flush();
                };
                expiry.Tick += delegate { expiry.Stop(); cover.Close(); };
                cover.Shown += delegate {
                    bool opaque = SetLayeredWindowAttributes(cover.Handle, 0, 255, 2);
                    bool placed = SetWindowPos(cover.Handle, new IntPtr(-1), target.Left, target.Top, target.Right - target.Left, target.Bottom - target.Top, 0x0010 | 0x0040);
                    if (!opaque || !placed) {
                        Console.WriteLine("{\"ready\":false,\"error_message\":\"Windows 未确认不透明置顶遮挡设置\"}");
                        Console.Out.Flush(); cover.Close(); return;
                    }
                    readiness.Start(); expiry.Start();
                };
                Application.Run(cover);
            }
        }
    }
}
'@

try {
    Add-Type -TypeDefinition $source -ReferencedAssemblies System.Windows.Forms, System.Drawing
    if ($SelfTest) {
        if (-not [GoodHRWindowsCover]::SelfTest()) { throw '窗口边界自检失败' }
        @{ ready = $false; self_test = 'passed'; window_operations = 0 } | ConvertTo-Json -Compress
        exit 0
    }
    Assert-IsolatedBrowser $BrowserPid
    [GoodHRWindowsCover]::Run($BrowserPid, $Seconds)
} catch {
    @{ ready = $false; error_message = $_.Exception.Message } | ConvertTo-Json -Compress
    exit 1
}
