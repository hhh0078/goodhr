# 文件作用说明：从项目 GoodHR PNG Logo 生成带多种尺寸的 Windows ICO，供安装器和快捷方式统一使用。
param(
  [string]$SourcePath = (Join-Path $PSScriptRoot "..\assets\goodhr-logo-transparent-512.png"),
  [string]$OutputPath = (Join-Path $PSScriptRoot "..\assets\goodhr-logo.ico")
)

$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.Drawing

# New-IconFrame 等比例缩放 Logo，并生成透明背景的正方形 PNG 图标帧。
function New-IconFrame {
  param([System.Drawing.Image]$Source, [int]$Size)
  $Bitmap = [System.Drawing.Bitmap]::new($Size, $Size, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
  $Graphics = [System.Drawing.Graphics]::FromImage($Bitmap)
  $Stream = [System.IO.MemoryStream]::new()
  try {
    $Graphics.Clear([System.Drawing.Color]::Transparent)
    $Graphics.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
    $Graphics.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::HighQuality
    $Graphics.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
    $Scale = [Math]::Min($Size / $Source.Width, $Size / $Source.Height)
    $Width = [Math]::Max(1, [int][Math]::Round($Source.Width * $Scale))
    $Height = [Math]::Max(1, [int][Math]::Round($Source.Height * $Scale))
    $Bounds = [System.Drawing.Rectangle]::new([int](($Size - $Width) / 2), [int](($Size - $Height) / 2), $Width, $Height)
    $Graphics.DrawImage($Source, $Bounds)
    $Bitmap.Save($Stream, [System.Drawing.Imaging.ImageFormat]::Png)
    return ,$Stream.ToArray()
  }
  finally {
    $Stream.Dispose()
    $Graphics.Dispose()
    $Bitmap.Dispose()
  }
}

$Source = [System.Drawing.Image]::FromFile((Resolve-Path -LiteralPath $SourcePath).Path)
$Frames = [System.Collections.Generic.List[byte[]]]::new()
$Sizes = @(16, 24, 32, 48, 64, 128, 256)
try {
  foreach ($Size in $Sizes) {
    $Frames.Add((New-IconFrame -Source $Source -Size $Size))
  }
}
finally {
  $Source.Dispose()
}

$ResolvedOutput = [System.IO.Path]::GetFullPath($OutputPath)
$Writer = [System.IO.BinaryWriter]::new([System.IO.File]::Create($ResolvedOutput))
try {
  $Writer.Write([uint16]0)
  $Writer.Write([uint16]1)
  $Writer.Write([uint16]$Frames.Count)
  $Offset = 6 + 16 * $Frames.Count
  for ($Index = 0; $Index -lt $Frames.Count; $Index++) {
    $Dimension = if ($Sizes[$Index] -eq 256) { [byte]0 } else { [byte]$Sizes[$Index] }
    $Writer.Write($Dimension)
    $Writer.Write($Dimension)
    $Writer.Write([byte]0)
    $Writer.Write([byte]0)
    $Writer.Write([uint16]1)
    $Writer.Write([uint16]32)
    $Writer.Write([uint32]$Frames[$Index].Length)
    $Writer.Write([uint32]$Offset)
    $Offset += $Frames[$Index].Length
  }
  foreach ($Frame in $Frames) {
    $Writer.Write($Frame)
  }
}
finally {
  $Writer.Dispose()
}
Write-Host "[GoodHR] Windows Logo 图标已生成：$ResolvedOutput"
