# Dev helper: screenshot the app window and click inside it, so the Wails UI
# can be driven and inspected locally. Not shipped.
param(
  [ValidateSet('shot', 'click', 'find')]
  [string]$Action = 'shot',
  [string]$Out = 'E:\claude\_shot.png',
  # Click coordinates are given as fractions of the window (0..1) so they stay
  # valid regardless of where the window happens to sit on screen.
  [double]$FX = 0,
  [double]$FY = 0,
  [string]$ProcName = 'GameSaver'
)

Add-Type -AssemblyName System.Windows.Forms, System.Drawing

Add-Type @"
using System;
using System.Runtime.InteropServices;
public class U {
  // Without this the shell hosting us is DPI-virtualised: GetWindowRect and
  // CopyFromScreen report scaled coordinates while the real window lives at
  // physical ones, so clicks land in the wrong place.
  [DllImport("user32.dll")] public static extern bool SetProcessDPIAware();
  [DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
  [DllImport("user32.dll")] public static extern void mouse_event(uint f, uint dx, uint dy, uint d, IntPtr e);
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
  [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h, int n);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  public struct RECT { public int Left, Top, Right, Bottom; }
}
"@

[void][U]::SetProcessDPIAware()

$proc = Get-Process -Name $ProcName -ErrorAction SilentlyContinue |
        Where-Object { $_.MainWindowHandle -ne 0 } | Select-Object -First 1
if (-not $proc) { "NO WINDOW for $ProcName"; exit 1 }
$h = $proc.MainWindowHandle
$r = New-Object U+RECT
[void][U]::GetWindowRect($h, [ref]$r)
$w = $r.Right - $r.Left
$ht = $r.Bottom - $r.Top

switch ($Action) {
  'find' { "hwnd=$h rect=$($r.Left),$($r.Top) size=${w}x${ht}" }
  'shot' {
    [void][U]::ShowWindow($h, 9)   # SW_RESTORE
    [void][U]::SetForegroundWindow($h)
    Start-Sleep -Milliseconds 800
    [void][U]::GetWindowRect($h, [ref]$r)
    $w = $r.Right - $r.Left; $ht = $r.Bottom - $r.Top
    $bmp = New-Object System.Drawing.Bitmap $w, $ht
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.CopyFromScreen($r.Left, $r.Top, 0, 0, $bmp.Size)
    $bmp.Save($Out, [System.Drawing.Imaging.ImageFormat]::Png)
    $g.Dispose(); $bmp.Dispose()
    "saved $Out (${w}x${ht})"
  }
  'click' {
    [void][U]::ShowWindow($h, 9)
    [void][U]::SetForegroundWindow($h)
    Start-Sleep -Milliseconds 400
    $x = [int]($r.Left + $w * $FX)
    $y = [int]($r.Top + $ht * $FY)
    # WebView2 wants to see the pointer arrive before it accepts the press:
    # a bare down/up at a fresh position is often swallowed, so move first,
    # let hover settle, then click with a real gap between down and up.
    [void][U]::SetCursorPos($x - 12, $y - 12)
    Start-Sleep -Milliseconds 120
    [void][U]::SetCursorPos($x, $y)
    Start-Sleep -Milliseconds 250
    [U]::mouse_event(0x0002, 0, 0, 0, [IntPtr]::Zero)  # LEFTDOWN
    Start-Sleep -Milliseconds 80
    [U]::mouse_event(0x0004, 0, 0, 0, [IntPtr]::Zero)  # LEFTUP
    Start-Sleep -Milliseconds 200
    "clicked $x,$y (window ${w}x${ht})"
  }
}
