"""Synthetic recovery setup on an owned test display; never save phrase pixels.

The caller creates a disposable root. Only that test root is read in memory,
through an OCR pipe, and entered with native XTest events. No public IPC,
clipboard, process argument/environment, report or screenshot carries its phrase.
"""
import io,re,subprocess,time

SECRET_TITLES=('Create local wallet','Unlock local wallet','Complete recovery setup',
               'Record recovery phrase','Confirm recovery phrase')

def secret_window(desktop):
 return any(title.startswith(SECRET_TITLES) for _,title in desktop.windows())

def record(desktop,expected=None,wrong=False):
 window,_=desktop.wait('Record recovery phrase',45);desktop.focus(window)
 x,y,w,h=desktop.geometry(window)
 # OCR consumes a crop in memory. In particular, do not use Desktop.screenshot
 # or put these bytes into a temporary file or exception diagnostic.
 found={}
 for attempt in range(3):
  time.sleep(.2)
  buffer=io.BytesIO();crop=desktop.image().crop((x,y,x+w,y+h));crop.resize((w*2,h*2)).save(buffer,format='PNG')
  output=subprocess.check_output(['tesseract','stdin','stdout','--psm','6'],input=buffer.getvalue(),stderr=subprocess.DEVNULL)
  found={int(i):word for i,word in re.findall(rb'(?m)^\s*(\d{1,2})[.),:]?\s*([a-z]+)\s*$',output)}
  if set(found)==set(range(1,13)):break
 if set(found)!=set(range(1,13)):raise RuntimeError('Disposable phrase could not be read safely; no phrase diagnostic retained')
 phrase=b' '.join(found[i] for i in range(1,13))
 if expected is not None and phrase!=expected:raise RuntimeError('Pending setup changed the disposable root')
 desktop.accept('Record recovery phrase')
 desktop.wait('Confirm recovery phrase',15)
 words=phrase.split();values=['incorrect']*3 if wrong else [words[i].decode('ascii') for i in (1,5,11)]
 desktop.inputs('Confirm recovery phrase',values);desktop.accept('Confirm recovery phrase')
 if wrong:
  desktop.wait('Recovery phrase not confirmed',15);desktop.accept('Recovery phrase not confirmed')
 time.sleep(.3)
 return phrase
