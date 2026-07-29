package export

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"time"

	"github.com/paulsmith/twee/internal/render"
)

// htmlSink streams PNG frames into a self-contained replay page. Keeping the
// temporary file beside the destination makes the final rename atomic on the
// filesystems supported by os.Rename.
type htmlSink struct {
	outPath  string
	tempPath string
	file     *os.File
	w        *bufio.Writer
	frames   int
}

func newHTMLSink(outPath string) (*htmlSink, error) {
	abs, err := filepath.Abs(outPath)
	if err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(filepath.Dir(abs), "."+filepath.Base(abs)+".*.tmp")
	if err != nil {
		return nil, err
	}
	s := &htmlSink{
		outPath:  abs,
		tempPath: f.Name(),
		file:     f,
		w:        bufio.NewWriterSize(f, 64*1024),
	}
	if _, err := s.w.WriteString(htmlPrefix); err != nil {
		s.abort()
		return nil, err
	}
	return s, nil
}

func (s *htmlSink) add(img *image.RGBA, d time.Duration) error {
	if s.frames > 0 {
		if _, err := s.w.WriteString(",\n"); err != nil {
			return err
		}
	}
	if _, err := s.w.WriteString(`{"src":"data:image/png;base64,`); err != nil {
		return err
	}
	enc := base64.NewEncoder(base64.StdEncoding, s.w)
	if err := render.EncodePNG(enc, img); err != nil {
		_ = enc.Close()
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, `","duration_ns":%d}`, d.Nanoseconds()); err != nil {
		return err
	}
	s.frames++
	return nil
}

func (s *htmlSink) close() error {
	if _, err := s.w.WriteString(htmlSuffix); err != nil {
		return err
	}
	if err := s.w.Flush(); err != nil {
		return err
	}
	if err := s.file.Sync(); err != nil {
		return err
	}
	if err := s.file.Close(); err != nil {
		return err
	}
	s.file = nil
	if err := preserveDestinationMode(s.tempPath, s.outPath); err != nil {
		return err
	}
	if err := os.Rename(s.tempPath, s.outPath); err != nil {
		return err
	}
	s.tempPath = ""
	return nil
}

// abort removes an uncommitted page. It is safe to call after close.
func (s *htmlSink) abort() {
	if s.file != nil {
		_ = s.file.Close()
		s.file = nil
	}
	if s.tempPath != "" {
		_ = os.Remove(s.tempPath)
		s.tempPath = ""
	}
}

const htmlPrefix = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'none'; media-src 'none'; object-src 'none'; frame-src 'none'; base-uri 'none'; form-action 'none'">
<title>twee replay</title>
<style>
:root { color-scheme: dark; font-family: ui-monospace, SFMono-Regular, Consolas, monospace; }
* { box-sizing: border-box; }
body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: #171717; color: #eee; }
main { width: min(100%, 1100px); padding: 1rem; }
.screen { display: grid; place-items: center; min-height: 12rem; overflow: auto; background: #000; border: 1px solid #444; }
#frame { display: block; max-width: 100%; height: auto; image-rendering: auto; }
.controls { display: grid; grid-template-columns: auto auto auto auto 1fr auto auto; gap: .5rem; align-items: center; padding-top: .75rem; }
button, select, input { font: inherit; }
button, select { padding: .35rem .55rem; color: inherit; background: #292929; border: 1px solid #666; border-radius: .25rem; }
button:hover { background: #383838; }
#timeline { width: 100%; }
#time { min-width: 12ch; text-align: right; font-variant-numeric: tabular-nums; }
@media (max-width: 720px) { .controls { grid-template-columns: repeat(4, auto); } #timeline { grid-column: 1 / -1; grid-row: 1; } #time { margin-left: auto; } }
</style>
</head>
<body>
<main>
<div class="screen"><canvas id="frame" role="img" aria-label="Recorded terminal frame"></canvas></div>
<div class="controls" aria-label="Replay controls">
<button id="restart" type="button" title="Restart (Home)">Restart</button>
<button id="previous" type="button" title="Previous frame (Left arrow)">Previous</button>
<button id="play" type="button" title="Play or pause (Space)">Play</button>
<button id="next" type="button" title="Next frame (Right arrow)">Next</button>
<input id="timeline" type="range" min="0" value="0" step="any" aria-label="Replay position">
<span id="time" aria-live="off">0:00.000 / 0:00.000</span>
<label>Speed <select id="speed" aria-label="Playback speed"><option value="0.25">0.25×</option><option value="0.5">0.5×</option><option value="1" selected>1×</option><option value="2">2×</option><option value="4">4×</option></select></label>
</div>
<script type="application/json" id="twee-frames">[
`

const htmlSuffix = `
]</script>
<script>
(() => {
  'use strict';
  const frames = JSON.parse(document.getElementById('twee-frames').textContent);
  const canvas = document.getElementById('frame');
  const context = canvas.getContext('2d');
  const playButton = document.getElementById('play');
  const timeline = document.getElementById('timeline');
  const timeLabel = document.getElementById('time');
  const speedSelect = document.getElementById('speed');
  const starts = [];
  let total = 0;
  for (const frame of frames) {
    starts.push(total);
    total += Number(frame.duration_ns) / 1000000;
  }
  let position = 0;
  let frameIndex = -1;
  let playing = false;
  let lastTick = 0;
  let animation = 0;
  const decoded = new Array(frames.length);

  timeline.max = String(total);

  function formatTime(milliseconds) {
    const value = Math.max(0, milliseconds);
    const minutes = Math.floor(value / 60000);
    const seconds = Math.floor(value / 1000) % 60;
    const millis = Math.floor(value % 1000);
    return minutes + ':' + String(seconds).padStart(2, '0') + '.' + String(millis).padStart(3, '0');
  }

  function indexAt(milliseconds) {
    if (frames.length === 0) return -1;
    if (milliseconds >= total) return frames.length - 1;
    let low = 0;
    let high = starts.length;
    while (low + 1 < high) {
      const mid = (low + high) >>> 1;
      if (starts[mid] <= milliseconds) low = mid;
      else high = mid;
    }
    return low;
  }

  function render() {
    position = Math.max(0, Math.min(total, position));
    const nextIndex = indexAt(position);
    if (nextIndex !== frameIndex) {
      frameIndex = nextIndex;
      drawFrame(frameIndex);
    }
    timeline.value = String(position);
    timeLabel.textContent = formatTime(position) + ' / ' + formatTime(total);
  }

  function drawFrame(index) {
    if (index < 0) return;
    let picture = decoded[index];
    if (!picture) {
      picture = new Image();
      decoded[index] = picture;
      picture.addEventListener('load', () => paintFrame(index, picture), { once: true });
      picture.src = frames[index].src;
      return;
    }
    if (picture.complete) paintFrame(index, picture);
  }

  function paintFrame(index, picture) {
    if (index !== frameIndex) return;
    canvas.width = picture.naturalWidth;
    canvas.height = picture.naturalHeight;
    context.drawImage(picture, 0, 0);
  }

  function setPlaying(value) {
    playing = value && frames.length > 0 && position < total;
    playButton.textContent = playing ? 'Pause' : 'Play';
    playButton.setAttribute('aria-pressed', String(playing));
    cancelAnimationFrame(animation);
    if (playing) {
      lastTick = performance.now();
      animation = requestAnimationFrame(tick);
    }
  }

  function tick(now) {
    if (!playing) return;
    position += (now - lastTick) * Number(speedSelect.value);
    lastTick = now;
    if (position >= total) {
      position = total;
      setPlaying(false);
      render();
      return;
    }
    render();
    animation = requestAnimationFrame(tick);
  }

  function seekFrame(index) {
    if (frames.length === 0) return;
    const wasPlaying = playing;
    setPlaying(false);
    position = starts[Math.max(0, Math.min(frames.length - 1, index))];
    render();
    if (wasPlaying) setPlaying(true);
  }

  playButton.addEventListener('click', () => {
    if (!playing && position >= total) position = 0;
    setPlaying(!playing);
    render();
  });
  document.getElementById('restart').addEventListener('click', () => {
    const wasPlaying = playing;
    setPlaying(false);
    position = 0;
    render();
    if (wasPlaying) setPlaying(true);
  });
  document.getElementById('previous').addEventListener('click', () => seekFrame(frameIndex - 1));
  document.getElementById('next').addEventListener('click', () => seekFrame(frameIndex + 1));
  timeline.addEventListener('input', () => {
    const wasPlaying = playing;
    setPlaying(false);
    position = Number(timeline.value);
    render();
    if (wasPlaying) setPlaying(true);
  });
  speedSelect.addEventListener('change', () => {
    if (playing) {
      lastTick = performance.now();
    }
  });
  document.addEventListener('keydown', event => {
    if (event.target === speedSelect || event.target === timeline) return;
    if (event.key === ' ') {
      event.preventDefault();
      playButton.click();
    } else if (event.key === 'Home') {
      event.preventDefault();
      document.getElementById('restart').click();
    } else if (event.key === 'ArrowLeft') {
      event.preventDefault();
      seekFrame(frameIndex - 1);
    } else if (event.key === 'ArrowRight') {
      event.preventDefault();
      seekFrame(frameIndex + 1);
    }
  });
  render();
})();
</script>
</main>
</body>
</html>
`
