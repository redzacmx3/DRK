#!/usr/bin/env python3
"""Build the embedded blank Excel skeleton; runtime Go replaces IDs and PNGs.
Developer tool only: xlsxwriter. No Python, macros, fonts or network at runtime.
"""
from pathlib import Path
import io, struct, zlib
import xlsxwriter

ALPHABET='0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ-. $/+%*'
PATTERNS=[0x034,0x121,0x061,0x160,0x031,0x130,0x070,0x025,0x124,0x064,0x109,0x049,0x148,0x019,0x118,0x058,0x00d,0x10c,0x04c,0x01c,0x103,0x043,0x142,0x013,0x112,0x052,0x007,0x106,0x046,0x016,0x181,0x0c1,0x1c0,0x091,0x190,0x0d0,0x085,0x184,0x0c4,0x0a8,0x0a2,0x08a,0x02a,0x094]
PREFIX='D-TEMPLATE0000'
ROWS=100
HEADERS=['Erfassungsnummer','Bezeichnung','Standort','Menge','Basiseinheit','Charge','Verfall','Fremdcode','Bemerkung','Artikel-ID']
def barcode(value):
 bits=[0]*10
 for char in '*'+value+'*':
  pattern=PATTERNS[ALPHABET.index(char)]
  for i in range(9):bits.extend([1 if i%2==0 else 0]*(3 if pattern & (1<<(8-i)) else 1))
  bits.append(0)
 bits.extend([0]*10)
 width=len(bits)*2;height=90
 raw=b''.join(b'\x00'+bytes(0 if bits[x//2] else 255 for x in range(width)) for _ in range(height))
 def chunk(kind,data):return struct.pack('>I',len(data))+kind+data+struct.pack('>I',zlib.crc32(kind+data)&0xffffffff)
 return b'\x89PNG\r\n\x1a\n'+chunk(b'IHDR',struct.pack('>IIBBBBB',width,height,8,0,0,0,0))+chunk(b'IDAT',zlib.compress(raw))+chunk(b'IEND',b'')

root=Path(__file__).resolve().parents[1]
path=root/'internal/app/templates/stock-intake.xlsx'
book=xlsxwriter.Workbook(path)
book.set_properties({'title':'Verbrauchsmaterial – schnelle Erfassung','author':'DRK Warenbestand','comments':'Leere Erfassungsvorlage; keine Betriebs- oder Prüfungsfreigabe.'})
start=book.add_worksheet('Start');inventory=book.add_worksheet('Inventar');labels=book.add_worksheet('Etiketten');scanner=book.add_worksheet('Scanner');sites=book.add_worksheet('Standorte')
title=book.add_format({'bold':True,'font_size':18,'font_color':'#9b1c31'})
head=book.add_format({'bold':True,'bg_color':'#9b1c31','font_color':'white','text_wrap':True})
text=book.add_format({'num_format':'@','locked':False,'valign':'top'})
locked=book.add_format({'num_format':'@','locked':True,'font_color':'#555555','bg_color':'#eeeeee'})
wrap=book.add_format({'text_wrap':True,'valign':'top'})
label=book.add_format({'align':'center','bold':True,'font_size':11})
start.set_column('A:A',3);start.set_column('B:B',110);start.write('B2','Schnell erfassen: Verbrauchsmaterial',title)
steps=[
 '1. Datei unter eigenem Namen speichern. Jede neue Vorlage aus dem Programm oder dem Befehl stock-template hat 100 eigene feste Erfassungsnummern. Eine Kopie oder derselbe GitHub-Download behält dieselben Nummern.',
 '2. Im Blatt Inventar eine Zeile je Artikel, Charge und Lagerort ausfüllen. Pflicht: Bezeichnung, Standort, Menge und Basiseinheit. Erfassungsnummern sind fest vorgegeben. Leere vorbereitete Zeilen werden beim Import ignoriert.',
 '3. Menge immer als positive ganze Zahl in Basiseinheiten angeben, z. B. 20 Stück. Eine Packung mit 10 Stück bedeutet Menge 10 und Basiseinheit Stück. Ein Verpackungscode berechnet keine Menge. Nullbestände hier auslassen.',
 '4. Charge und Verfall soweit bekannt ergänzen. Verfall als Text JJJJ-MM-TT, z. B. 2028-12-31. Nicht raten und keine Monatsdaten automatisch zum Monatsende erweitern. Unbekannte Charge oder unbekannter/abgelaufener Verfall wird in Quarantäne gebucht.',
 '5. Fremdcode nimmt vorhandene Hersteller-/Verpackungscodes per USB-Handscanner auf, z. B. PZN oder GTIN. Alle Eingabefelder sind Text, damit führende Nullen bleiben. Der Code allein identifiziert noch keinen Artikel und keine Packungsgröße im Programm.',
 '6. Die erzeugten Etiketten enthalten Erfassungsnummern für diese Liste, keine Hersteller- oder Chargenkennzeichnung. Optional am Erfassungsblatt oder Lagerbehälter verwenden; Originalkennzeichnungen nicht überkleben. Auf 100 % drucken und einen Barcode testen.',
 '7. USB-Handscanner als Tastatur, Code 39 und Enter-Abschluss einstellen. Im Blatt Scanner Zelle B3 wählen, Erfassungsnummer oder Fremdcode scannen. Die Suche zeigt Grunddaten; mehrdeutige Codes werden gemeldet. Ein Scan erzeugt keine Buchung.',
 '8. Standorte frei eintragen oder im Blatt Standorte sammeln. Beim Import ausdrücklich vorhandenen Programm-Lagerorten zuordnen. Für bestehende Artikel müssen Bezeichnung und Basiseinheit passen; bei mehrdeutigen Namen die Artikel-ID aus dem Katalog ergänzen.',
 '9. Nur das Blatt Inventar als CSV UTF-8 speichern; die XLSX zusätzlich behalten. Im Programm Material-CSV öffnen und Importart wählen: Erstbestand für noch nicht erfasste Bestände; späterer Zugang nur für tatsächliche neue Lieferungen.',
 '10. Vorschau prüfen: neue Artikel/Chargen, Menge, Standort und Quarantäne. Erstbestand wird abgelehnt, wenn der Artikelbestand am Ort schon erfasst wurde, auch mit Menge null. Für eine Bestandszählung die Bestandskorrektur im Programm verwenden, keine zusätzliche Eingangsbuchung.',
 '11. Dieselbe Erfassungsnummer mit denselben Daten wird übersprungen, auch nach Entnahme oder Storno. Geänderte Daten zur gleichen Nummer werden abgelehnt. Für neue Lieferungen neue unbenutzte Erfassungsnummern verwenden; nicht alte Zeilen umschreiben.',
 'Grenzen: Überblick und dokumentierter Zugang, keine automatische Nutzbarkeitsfreigabe oder Packungsumrechnung. Die Tabelle bleibt nach dem Export offline; Bestandsänderungen im Programm werden nicht in Excel zurückgeschrieben. Keine Makros oder Barcode-Schriftarten.'
]
for i,line in enumerate(steps,4):start.write(i,1,line,wrap);start.set_row(i,58 if i in [4,9,12] else 48)
start.hide_gridlines(2);start.freeze_panes(4,0)
widths=[24,28,26,15,18,22,20,28,40,38]
for i,width in enumerate(widths):inventory.set_column(i,i,width,locked if i==0 else text)
for i,header in enumerate(HEADERS):inventory.write(0,i,header,head)
for row in range(1,ROWS+1):
 inventory.write_string(row,0,f'{PREFIX}-{row:03d}',locked)
 for column in range(1,10):inventory.write_blank(row,column,None,text)
inventory.freeze_panes(1,3);inventory.autofilter(0,0,ROWS,9);inventory.set_row(0,32)
book.define_name('Standortliste',"='Standorte'!$A$2:$A$101")
inventory.data_validation(1,2,ROWS,2,{'validate':'list','source':'=Standortliste','error_type':'warning','error_title':'Freier Standort','error_message':'Freien Standort verwenden? Er kann später beim Import zugeordnet werden.'})
inventory.protect('',{'autofilter':True,'select_locked_cells':True,'select_unlocked_cells':True})
sites.set_column('A:A',40,text);sites.set_column('B:B',90);sites.write('A1','Standortnamen',head);sites.write('B2','Hier eigene Standortnamen sammeln, z. B. Fahrzeug 1, Lager oder Tasche. Keine UUID nötig.',wrap)
for row in range(1,101):sites.write_blank(row,0,None,text)
labels.hide_gridlines(2);labels.set_column('A:F',7.2);labels.set_column('G:G',3);labels.set_column('H:M',7.2)
labels.set_portrait();labels.set_paper(9);labels.set_margins(.25,.25,.3,.3);labels.set_print_scale(100);labels.print_area(0,0,ROWS//2*4-1,12)
labels.set_header('&CErfassungsetiketten · Code 39 · tatsächliche Größe');labels.set_footer('&CErfassungsnummer – keine Produkt- oder Chargenkennzeichnung')
for i in range(ROWS):
 row=(i//2)*4;column=0 if i%2==0 else 7;identifier=f'{PREFIX}-{i+1:03d}'
 labels.merge_range(row,column,row,column+5,'',label);labels.write_formula(row,column,f'=Inventar!A{i+2}',label,identifier)
 labels.set_row(row+1,35);labels.set_row(row+2,22);labels.set_row(row+3,12)
 labels.insert_image(row+1,column,'barcode.png',{'image_data':io.BytesIO(barcode(identifier)),'x_scale':.48,'y_scale':.48,'object_position':1,'description':'Code 39: '+identifier})
 labels.merge_range(row+2,column,row+2,column+5,'',label);labels.write_formula(row+2,column,f'=IF(Inventar!B{i+2}="","Noch nicht erfasst",Inventar!B{i+2})',label,'Noch nicht erfasst')
labels.protect('')
scanner.set_column('A:A',22);scanner.set_column('B:B',65);scanner.set_column('C:C',35)
scanner.write('A1','Barcode suchen',title);scanner.write('A3','Hier scannen →',head);scanner.write_blank('B3',None,text)
scanner.write('B4','B3 wählen, scannen, Enter; USB-Scanner sendet Tastatureingabe.',wrap)
scanner.write_formula('C2','=SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(B3,"~","~~"),"*","~*"),"?","~?")',None,'')
scanner.write_formula('B5','=IF(B3="","",IF(COUNTIF(Inventar!$A$2:$A$101,$C$2)+COUNTIF(Inventar!$H$2:$H$101,$C$2)>1,"Mehrdeutiger Code – Inventar prüfen",IF(COUNTIF(Inventar!$A$2:$A$101,$C$2)+COUNTIF(Inventar!$H$2:$H$101,$C$2)=0,"Code nicht gefunden","Gefunden")))',wrap,'')
scanner.write_formula('C3','=IFERROR(MATCH($C$2,Inventar!$A$2:$A$101,0),IFERROR(MATCH($C$2,Inventar!$H$2:$H$101,0),0))',None,0);scanner.set_column('C:C',None,None,{'hidden':True})
for column,header in enumerate(HEADERS):
 row=7+column;scanner.write(row,0,header,head)
 letter=xlsxwriter.utility.xl_col_to_name(column)
 formula=f'=IF(OR($B$3="",$B$5<>"Gefunden"),"",IF(INDEX(Inventar!${letter}$2:${letter}$101,$C$3)="","",INDEX(Inventar!${letter}$2:${letter}$101,$C$3)))'
 scanner.write_formula(row,1,formula,wrap,'')
scanner.protect('',{'select_locked_cells':False,'select_unlocked_cells':True});scanner.set_selection('B3');scanner.hide_gridlines(2)
book.close();print(path.relative_to(root))
