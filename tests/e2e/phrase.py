"""Synthetic recovery setup on an owned test display; never save phrase pixels.

The caller creates a disposable root. Only that test root is read in memory,
through an OCR pipe and entered with XTest events. Secret transport is limited
to the approved prompt path. Clipboard, command arguments, environment, logs,
reports and screenshots never retain the phrase.
"""
import io,re,subprocess,time

SECRET_TITLES=('Create local wallet','Unlock local wallet','Complete recovery setup',
               'Record recovery phrase','Confirm recovery phrase','Change app storage password','Configure trusted RPC')

def secret_window(desktop):
 return desktop.secret_window()

def record(desktop,expected=None,wrong=False):
 window,_=desktop.wait('Record recovery phrase',45);desktop.focus(window)
 x,y,w,h=desktop.geometry(window)
 # OCR consumes a crop in memory. In particular, do not use Desktop.screenshot
 # or put these bytes into a temporary file or exception diagnostic.
 found={}
 for attempt in range(3):
  time.sleep(.2)
  buffer=io.BytesIO();crop=desktop.image().crop((x,y,x+w,y+h))
  if desktop.wait('Record recovery phrase')[1].startswith('LocalWallet — '):
   from PIL import ImageOps
   crop=ImageOps.invert(crop.convert('L'))
  crop.resize((w*2,h*2)).save(buffer,format='PNG')
  output=subprocess.check_output(['tesseract','stdin','stdout','--psm','6'],input=buffer.getvalue(),stderr=subprocess.DEVNULL)
  found={int(i):word for i,word in re.findall(rb'(?m)\b(\d{1,2})[.),:]?\s+([a-z]+)\b',output)}
  if set(found)==set(range(1,13)):break
 if set(found)!=set(range(1,13)) and desktop.wait('Record recovery phrase')[1].startswith('LocalWallet — '):
  # Read each displayed grid cell independently when document OCR merges the
  # small ordinal with a word. Locate public CSS borders; secret pixels stay
  # exclusively in memory. Recovered addresses later verify the full phrase.
  from PIL import ImageOps
  picture=desktop.image().crop((x,y,x+w,y+h)).convert('RGB')
  cell=(w-60-18)//3;lefts=[30+i*(cell+9) for i in range(3)]
  rows=[yy for yy in range(h-100) if all(max(abs(v-t) for v,t in zip(picture.getpixel((xx,yy)),(52,74,99)))<12 for xx in lefts)]
  groups=[]
  for yy in rows:
   if not groups or yy!=groups[-1][-1]+1:groups.append([])
   groups[-1].append(yy)
  groups=[group for group in groups if len(group)>=15]
  if len(groups)==4:
   parsed={}
   for row,group in enumerate(groups):
    center=group[len(group)//2]
    for column,left in enumerate(lefts):
     cell_image=ImageOps.invert(picture.crop((left+35,center-15,left+cell-6,center+15)).convert('L'))
     temporary=io.BytesIO();cell_image.resize((cell_image.width*3,cell_image.height*3)).save(temporary,format='PNG')
     value=subprocess.check_output(['tesseract','stdin','stdout','--psm','7','-c','tessedit_char_whitelist=abcdefghijklmnopqrstuvwxyz'],input=temporary.getvalue(),stderr=subprocess.DEVNULL).strip().lower()
     if re.fullmatch(rb'[a-z]+',value):parsed[row*3+column+1]=value
   if len(parsed)==12:found=parsed
 if set(found)!=set(range(1,13)):raise RuntimeError('Disposable phrase OCR incomplete: '+str(len(found))+' numbered words; '+str(len(groups) if 'groups' in locals() else 0)+' grid rows; '+str(len(parsed) if 'parsed' in locals() else 0)+' cells; no word/pixel diagnostic retained')
 phrase=b' '.join(found[i] for i in range(1,13))
 if expected is not None and phrase!=expected:raise RuntimeError('Pending setup changed the disposable root')
 desktop.accept('Record recovery phrase')
 desktop.wait('Confirm recovery phrase',15)
 words=phrase.split();values=['incorrect']*3 if wrong else [words[i].decode('ascii') for i in (1,5,11)]
 desktop.inputs('Confirm recovery phrase',values);desktop.accept('Confirm recovery phrase')
 # A wrong challenge is reported by the ordinary fixed bottom status bar.
 # The caller verifies its safe public message after the private input closes;
 # no information-only modal is opened or automatically accepted here.
 time.sleep(.3)
 return phrase
