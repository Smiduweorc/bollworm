package docx

import "strconv"

// The values below are Word 365's own defaults for a blank document (Aptos,
// the 0F4761 heading blue, 8pt after a paragraph, 1.16 line spacing), with
// three changes a person writing a reference manual would make: body text at
// 11pt rather than 12, a page break before every chapter, and smaller text in
// tables.
var styles = xmlHeader + `<w:styles ` + nsW + `>` +
	`<w:docDefaults>` +
	`<w:rPrDefault><w:rPr><w:rFonts w:ascii="Aptos" w:eastAsia="Aptos" w:hAnsi="Aptos" w:cs="Aptos"/><w:kern w:val="2"/><w:sz w:val="22"/><w:szCs w:val="22"/></w:rPr></w:rPrDefault>` +
	`<w:pPrDefault><w:pPr><w:spacing w:after="160" w:line="278" w:lineRule="auto"/></w:pPr></w:pPrDefault>` +
	`</w:docDefaults>` +

	`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>` +
	`<w:style w:type="character" w:default="1" w:styleId="DefaultParagraphFont"><w:name w:val="Default Paragraph Font"/><w:uiPriority w:val="1"/><w:semiHidden/><w:unhideWhenUsed/></w:style>` +
	`<w:style w:type="table" w:default="1" w:styleId="TableNormal"><w:name w:val="Normal Table"/><w:uiPriority w:val="99"/><w:semiHidden/><w:unhideWhenUsed/><w:tblPr><w:tblInd w:w="0" w:type="dxa"/><w:tblCellMar><w:top w:w="0" w:type="dxa"/><w:left w:w="108" w:type="dxa"/><w:bottom w:w="0" w:type="dxa"/><w:right w:w="108" w:type="dxa"/></w:tblCellMar></w:tblPr></w:style>` +
	`<w:style w:type="numbering" w:default="1" w:styleId="NoList"><w:name w:val="No List"/><w:uiPriority w:val="99"/><w:semiHidden/><w:unhideWhenUsed/></w:style>` +

	`<w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:link w:val="TitleChar"/><w:uiPriority w:val="10"/><w:qFormat/>` +
	`<w:pPr><w:spacing w:before="2400" w:after="80" w:line="240" w:lineRule="auto"/><w:contextualSpacing/></w:pPr>` +
	`<w:rPr><w:rFonts w:ascii="Aptos Display" w:eastAsia="Aptos Display" w:hAnsi="Aptos Display" w:cs="Aptos Display"/><w:spacing w:val="-10"/><w:kern w:val="28"/><w:sz w:val="56"/><w:szCs w:val="56"/></w:rPr></w:style>` +
	`<w:style w:type="character" w:customStyle="1" w:styleId="TitleChar"><w:name w:val="Title Char"/><w:basedOn w:val="DefaultParagraphFont"/><w:link w:val="Title"/><w:uiPriority w:val="10"/>` +
	`<w:rPr><w:rFonts w:ascii="Aptos Display" w:eastAsia="Aptos Display" w:hAnsi="Aptos Display" w:cs="Aptos Display"/><w:spacing w:val="-10"/><w:kern w:val="28"/><w:sz w:val="56"/><w:szCs w:val="56"/></w:rPr></w:style>` +

	`<w:style w:type="paragraph" w:styleId="Subtitle"><w:name w:val="Subtitle"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:uiPriority w:val="11"/><w:qFormat/>` +
	`<w:pPr><w:spacing w:after="480"/></w:pPr>` +
	`<w:rPr><w:color w:val="595959"/><w:spacing w:val="15"/><w:sz w:val="28"/><w:szCs w:val="28"/></w:rPr></w:style>` +

	heading(1, 40, 360, true) +
	heading(2, 32, 280, false) +
	heading(3, 26, 200, false) +

	`<w:style w:type="paragraph" w:styleId="TOCHeading"><w:name w:val="TOC Heading"/><w:basedOn w:val="Heading1"/><w:next w:val="Normal"/><w:uiPriority w:val="39"/><w:unhideWhenUsed/><w:qFormat/>` +
	`<w:pPr><w:outlineLvl w:val="9"/></w:pPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="TOC1"><w:name w:val="toc 1"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:uiPriority w:val="39"/><w:unhideWhenUsed/>` +
	`<w:pPr><w:spacing w:after="100"/></w:pPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="TOC2"><w:name w:val="toc 2"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:uiPriority w:val="39"/><w:unhideWhenUsed/>` +
	`<w:pPr><w:spacing w:after="100"/><w:ind w:left="220"/></w:pPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="TOC3"><w:name w:val="toc 3"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:uiPriority w:val="39"/><w:unhideWhenUsed/>` +
	`<w:pPr><w:spacing w:after="100"/><w:ind w:left="440"/></w:pPr></w:style>` +

	`<w:style w:type="paragraph" w:styleId="ListParagraph"><w:name w:val="List Paragraph"/><w:basedOn w:val="Normal"/><w:uiPriority w:val="34"/><w:qFormat/>` +
	`<w:pPr><w:ind w:left="720"/><w:contextualSpacing/></w:pPr></w:style>` +

	`<w:style w:type="paragraph" w:styleId="Quote"><w:name w:val="Quote"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:uiPriority w:val="29"/><w:qFormat/>` +
	`<w:pPr><w:spacing w:before="160"/><w:ind w:left="720" w:right="720"/></w:pPr>` +
	`<w:rPr><w:i/><w:iCs/><w:color w:val="404040"/></w:rPr></w:style>` +

	`<w:style w:type="paragraph" w:customStyle="1" w:styleId="Code"><w:name w:val="Code"/><w:basedOn w:val="Normal"/><w:uiPriority w:val="99"/><w:qFormat/>` +
	`<w:pPr><w:pBdr><w:top w:val="single" w:sz="4" w:space="4" w:color="D9D9D9"/><w:left w:val="single" w:sz="4" w:space="4" w:color="D9D9D9"/><w:bottom w:val="single" w:sz="4" w:space="4" w:color="D9D9D9"/><w:right w:val="single" w:sz="4" w:space="4" w:color="D9D9D9"/></w:pBdr>` +
	`<w:shd w:val="clear" w:color="auto" w:fill="F5F5F5"/><w:spacing w:before="80" w:after="240" w:line="240" w:lineRule="auto"/><w:ind w:left="113" w:right="113"/></w:pPr>` +
	`<w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/><w:noProof/><w:sz w:val="18"/><w:szCs w:val="18"/></w:rPr></w:style>` +
	`<w:style w:type="character" w:customStyle="1" w:styleId="CodeChar"><w:name w:val="Inline Code"/><w:basedOn w:val="DefaultParagraphFont"/><w:uiPriority w:val="99"/><w:qFormat/>` +
	`<w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/><w:noProof/><w:color w:val="1F1F1F"/><w:sz w:val="20"/><w:szCs w:val="20"/></w:rPr></w:style>` +

	`<w:style w:type="paragraph" w:customStyle="1" w:styleId="TableText"><w:name w:val="Table Text"/><w:basedOn w:val="Normal"/><w:uiPriority w:val="99"/><w:qFormat/>` +
	`<w:pPr><w:spacing w:before="40" w:after="40" w:line="240" w:lineRule="auto"/></w:pPr>` +
	`<w:rPr><w:sz w:val="20"/><w:szCs w:val="20"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:customStyle="1" w:styleId="AfterTable"><w:name w:val="After Table"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:uiPriority w:val="99"/><w:semiHidden/>` +
	`<w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr></w:style>` +

	`<w:style w:type="paragraph" w:customStyle="1" w:styleId="Endpoint"><w:name w:val="Endpoint"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:uiPriority w:val="99"/><w:qFormat/>` +
	`<w:pPr><w:keepNext/><w:pBdr><w:left w:val="single" w:sz="24" w:space="6" w:color="0F4761"/></w:pBdr><w:shd w:val="clear" w:color="auto" w:fill="F2F6FA"/><w:spacing w:before="120" w:after="200" w:line="240" w:lineRule="auto"/><w:ind w:left="170"/></w:pPr>` +
	`<w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/><w:noProof/><w:sz w:val="21"/><w:szCs w:val="21"/></w:rPr></w:style>` +

	`<w:style w:type="paragraph" w:customStyle="1" w:styleId="Label"><w:name w:val="Label"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:uiPriority w:val="99"/><w:qFormat/>` +
	`<w:pPr><w:keepNext/><w:spacing w:before="120" w:after="60"/></w:pPr>` +
	`<w:rPr><w:b/><w:bCs/><w:color w:val="404040"/><w:sz w:val="20"/><w:szCs w:val="20"/></w:rPr></w:style>` +

	`<w:style w:type="character" w:styleId="Hyperlink"><w:name w:val="Hyperlink"/><w:basedOn w:val="DefaultParagraphFont"/><w:uiPriority w:val="99"/><w:unhideWhenUsed/>` +
	`<w:rPr><w:color w:val="467886"/><w:u w:val="single"/></w:rPr></w:style>` +

	`<w:style w:type="paragraph" w:styleId="Footer"><w:name w:val="footer"/><w:basedOn w:val="Normal"/><w:uiPriority w:val="99"/><w:unhideWhenUsed/>` +
	`<w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr>` +
	`<w:rPr><w:color w:val="595959"/><w:sz w:val="18"/><w:szCs w:val="18"/></w:rPr></w:style>` +

	`<w:style w:type="table" w:styleId="TableGrid"><w:name w:val="Table Grid"/><w:basedOn w:val="TableNormal"/><w:uiPriority w:val="39"/>` +
	`<w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr>` +
	`<w:tblPr><w:tblBorders><w:top w:val="single" w:sz="4" w:space="0" w:color="A6A6A6"/><w:left w:val="single" w:sz="4" w:space="0" w:color="A6A6A6"/><w:bottom w:val="single" w:sz="4" w:space="0" w:color="A6A6A6"/><w:right w:val="single" w:sz="4" w:space="0" w:color="A6A6A6"/><w:insideH w:val="single" w:sz="4" w:space="0" w:color="A6A6A6"/><w:insideV w:val="single" w:sz="4" w:space="0" w:color="A6A6A6"/></w:tblBorders></w:tblPr></w:style>` +
	`</w:styles>`

// heading builds Word's "heading N" style. size is in half-points and
// before in twentieths of a point.
func heading(level, size, before int, newPage bool) string {
	n := strconv.Itoa(level)
	font := `<w:rFonts w:ascii="Aptos Display" w:eastAsia="Aptos Display" w:hAnsi="Aptos Display" w:cs="Aptos Display"/>`
	if level == 3 {
		font = ""
	}
	page := ""
	if newPage {
		page = `<w:pageBreakBefore/>`
	}
	return `<w:style w:type="paragraph" w:styleId="Heading` + n + `"><w:name w:val="heading ` + n + `"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:uiPriority w:val="9"/><w:qFormat/>` +
		`<w:pPr><w:keepNext/><w:keepLines/>` + page + `<w:spacing w:before="` + strconv.Itoa(before) + `" w:after="80"/><w:outlineLvl w:val="` + strconv.Itoa(level-1) + `"/></w:pPr>` +
		`<w:rPr>` + font + `<w:color w:val="0F4761"/><w:sz w:val="` + strconv.Itoa(size) + `"/><w:szCs w:val="` + strconv.Itoa(size) + `"/></w:rPr></w:style>`
}

const settings = xmlHeader + `<w:settings ` + nsW + `>` +
	`<w:zoom w:percent="100"/>` +
	`<w:defaultTabStop w:val="720"/>` +
	`<w:characterSpacingControl w:val="doNotCompress"/>` +
	// Fills in the table of contents' page numbers when the file is opened.
	// Word asks before it does this; see the README's known quirks.
	`<w:updateFields w:val="true"/>` +
	// Mode 15 is Word 2013 and later. Without it Word opens the file in
	// Compatibility Mode, which is the first thing a reader notices.
	`<w:compat><w:compatSetting w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"/>` +
	`<w:compatSetting w:name="overrideTableStyleFontSizeAndJustification" w:uri="http://schemas.microsoft.com/office/word" w:val="1"/>` +
	`<w:compatSetting w:name="enableOpenTypeFeatures" w:uri="http://schemas.microsoft.com/office/word" w:val="1"/>` +
	`<w:compatSetting w:name="doNotFlipMirrorIndents" w:uri="http://schemas.microsoft.com/office/word" w:val="1"/>` +
	`</w:compat>` +
	`</w:settings>`

// Describes Aptos as a sans-serif family. Where Aptos is missing LibreOffice
// falls back to a serif font without this; with it, to a sans one.
const fontTable = xmlHeader + `<w:fonts ` + nsW + `>` +
	`<w:font w:name="Aptos"><w:panose1 w:val="020B0004020202020204"/><w:charset w:val="00"/><w:family w:val="swiss"/><w:pitch w:val="variable"/></w:font>` +
	`<w:font w:name="Aptos Display"><w:panose1 w:val="020B0004020202020204"/><w:charset w:val="00"/><w:family w:val="swiss"/><w:pitch w:val="variable"/></w:font>` +
	`</w:fonts>`
