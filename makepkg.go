package xsd

import (
	"fmt"
	"path"
	"strings"

	"github.com/metaleap/go-util-misc"
	"github.com/metaleap/go-util-slice"
	"github.com/metaleap/go-util-str"

	xsdt "github.com/metaleap/go-xsd/types"
)

var (
	PkgGen = &pkgGen{
		BaseCodePath:             ugo.GopathSrcGithub("metaleap", "go-xsd-pkg"),
		BasePath:                 "github.com/metaleap/go-xsd-pkg",
		ForceParseForDefaults:    false,
		PluralizeSpecialPrefixes: []string{"Library", "Instance"},
		AddWalkers:               true,
	}
	typeRenderRepls = map[string]string{"*": "", "[": "", "]": "", "(list ": "", ")": ""}
)

type pkgGen struct {
	BaseCodePath, BasePath   string
	ForceParseForDefaults    bool
	PluralizeSpecialPrefixes []string
	AddWalkers               bool
}

type beforeAfterMake interface {
	afterMakePkg(*PkgBag)
	beforeMakePkg(*PkgBag)
}

type pkgStack []interface{}

func (me *pkgStack) Pop() (el interface{}) { sl := *me; el = sl[0]; *me = sl[1:]; return }

func (me *pkgStack) Push(el interface{}) { nu := []interface{}{el}; *me = append(nu, *me...) }

type pkgStacks struct {
	Name, SimpleType pkgStack
}

func (me *pkgStacks) CurName() (r xsdt.NCName) {
	if len(me.Name) > 0 {
		r = me.Name[0].(xsdt.NCName)
	}
	return
}

func (me *pkgStacks) CurSimpleType() (r *SimpleType) {
	if len(me.SimpleType) > 0 {
		r = me.SimpleType[0].(*SimpleType)
	}
	return
}

func (me *pkgStacks) FullName() (r string) {
	for _, name := range me.Name {
		r += name.(xsdt.NCName).String()
	}
	return
}

type PkgBag struct {
	Schema *Schema
	Stacks pkgStacks

	allAtts       []*Attribute
	allAttGroups  []*AttributeGroup
	allElems      []*Element
	allElemGroups []*Group
	allNotations  []*Notation

	ctd                                                                                          *declType
	lines                                                                                        []string
	impName                                                                                      string
	debug                                                                                        bool
	imports, attsCache, elemsCacheOnce, elemsCacheMult, simpleBaseTypes, simpleContentValueTypes map[string]string
	impsUsed, elemsWritten, parseTypes, walkerTypes, declConvs                                   map[string]bool
	anonCounts                                                                                   map[string]uint64
	attGroups, attGroupRefImps                                                                   map[*AttributeGroup]string
	attsKeys, attRefImps                                                                         map[*Attribute]string
	declTypes                                                                                    map[string]*declType
	declElemTypes                                                                                map[element][]*declType
	declWrittenTypes                                                                             []*declType
	elemGroups, elemGroupRefImps                                                                 map[*Group]string
	elemChoices, elemChoiceRefImps                                                               map[*Choice]string
	elemSeqs, elemSeqRefImps                                                                     map[*Sequence]string
	elemKeys, elemRefImps                                                                        map[*Element]string
}

func newPkgBag(schema *Schema) (bag *PkgBag) {
	var newImpname = true
	bag = &PkgBag{Schema: schema}
	bag.impName = "xsdt"
	for i := 0; newImpname; i++ {
		newImpname = false
		loadedSchemas := make(map[string]bool)
		for _, s := range schema.allSchemas(loadedSchemas) {
			for ns, _ := range s.XMLNamespaces {
				if ns == bag.impName {
					newImpname = true
					break
				}
			}
		}
		if newImpname {
			bag.impName = sfmt("xsdt", i)
		}
	}
	bag.imports, bag.impsUsed, bag.lines = map[string]string{}, map[string]bool{}, []string{"//\tAuto-generated by the \"go-xsd\" package located at:", "//\t\tgithub.com/metaleap/go-xsd", "//\tComments on types and fields (if any) are from the XSD file located at:", "//\t\t" + bag.Schema.loadUri, "package go_" + bag.safeName(ustr.Replace(path.Base(bag.Schema.RootSchema([]string{bag.Schema.loadUri}).loadUri), map[string]string{"xsd": "", "schema": ""})), ""}
	bag.imports[bag.impName] = "github.com/metaleap/go-xsd/types"
	bag.anonCounts, bag.declTypes, bag.declElemTypes = map[string]uint64{}, map[string]*declType{}, map[element][]*declType{}
	bag.simpleContentValueTypes, bag.attsCache, bag.elemsCacheOnce, bag.elemsCacheMult, bag.simpleBaseTypes = map[string]string{}, map[string]string{}, map[string]string{}, map[string]string{}, map[string]string{}
	bag.attGroups, bag.attGroupRefImps = map[*AttributeGroup]string{}, map[*AttributeGroup]string{}
	bag.attsKeys, bag.attRefImps = map[*Attribute]string{}, map[*Attribute]string{}
	bag.elemGroups, bag.elemGroupRefImps = map[*Group]string{}, map[*Group]string{}
	bag.elemKeys, bag.elemRefImps = map[*Element]string{}, map[*Element]string{}
	bag.elemsWritten, bag.parseTypes, bag.walkerTypes, bag.declConvs = map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, pt := range []string{"Boolean", "Byte", "Double", "Float", "Int", "Integer", "Long", "NegativeInteger", "NonNegativeInteger", "NonPositiveInteger", "PositiveInteger", "Short", "UnsignedByte", "UnsignedInt", "UnsignedLong", "UnsignedShort"} {
		bag.parseTypes[bag.impName+"."+pt] = true
	}
	bag.addType(nil, idPrefix+"HasCdata", "").addField(nil, idPrefix+"CDATA", "string", ",chardata")
	return
}

func (me *PkgBag) addType(elem element, n, t string, a ...*Annotation) (dt *declType) {
	dt = &declType{elem: elem, Name: n, Type: t, Annotations: a}
	dt.Embeds, dt.Fields, dt.Methods, dt.memberWritten = map[string]*declEmbed{}, map[string]*declField{}, map[string]*declMethod{}, map[string]bool{}
	me.ctd, me.declTypes[n] = dt, dt
	if elem != nil {
		me.declElemTypes[elem] = append(me.declElemTypes[elem], dt)
	}
	return
}

func (me *PkgBag) AnonName(n string) (an xsdt.NCName) {
	var c uint64
	n = "Txsd" + n
	an = xsdt.NCName(n)
	if c = me.anonCounts[n]; c > 0 {
		an += xsdt.NCName(fmt.Sprintf("%v", c))
	}
	me.anonCounts[n] = c + 1
	return
}

func (me *PkgBag) append(lines ...string) {
	me.lines = append(me.lines, lines...)
}

func (me *PkgBag) appendFmt(addLineAfter bool, format string, fmtArgs ...interface{}) {
	me.append(fmt.Sprintf(format, fmtArgs...))
	if addLineAfter {
		me.append("")
	}
}

func (me *PkgBag) assembleSource() string {
	var (
		dt     *declType
		render = func(el element) {
			for _, dt = range me.declElemTypes[el] {
				if dt != nil {
					dt.render(me)
				}
			}
		}
		initLines = me.lines
		snConv    string
	)
	me.lines = []string{}
	loadedSchemas := make(map[string]bool)
	me.Schema.collectGlobals(me, loadedSchemas)
	if len(me.allNotations) > 0 {
		me.impsUsed[me.impName] = true
		me.appendFmt(false, "var %sNotations = new(%s.Notations)\n\nfunc init () {", idPrefix, me.impName)
		for _, not := range me.allNotations {
			not.makePkg(me)
		}
		me.appendFmt(true, "}")
	}
	for _, att := range me.allAtts {
		render(att)
	}
	for _, attGr := range me.allAttGroups {
		render(attGr)
	}
	for _, el := range me.allElems {
		render(el)
	}
	for _, gr := range me.allElemGroups {
		render(gr)
	}

  for _, dt := range me.declTypes {
    dt.render(me)
  }

	if len(me.walkerTypes) > 0 {
		doc := sfmt("//\tProvides %v strong-typed hooks for your own custom handler functions to be invoked when the Walk() method is called on any instance of any (non-attribute-related) struct type defined in this package.\n//\tIf your custom handler does get called at all for a given struct instance, then it always gets called twice, first with the 'enter' bool argument set to true, then (after having Walk()ed all subordinate struct instances, if any) once again with it set to false.", len(me.walkerTypes))
		me.appendFmt(true, `var (
	//	Set this to false to break a Walk() immediately as soon as the first error is returned by a custom handler function.
	//	If true, Walk() proceeds and accumulates all errors in the WalkErrors slice.
	WalkContinueOnError = true
	//	Contains all errors accumulated during Walk()s. If you're using this, you need to reset this yourself as needed prior to a fresh Walk().
	WalkErrors          []error
	//	Your custom error-handling function, if required.
	WalkOnError         func(error)
	%s
	WalkHandlers        = &%sWalkHandlers {}
)`, doc, idPrefix)
		me.appendFmt(false, doc)
		me.appendFmt(false, "type %vWalkHandlers struct {", idPrefix)
		for wt, _ := range me.walkerTypes {
			me.appendFmt(false, "\t%s func (*%s, bool) (error)", wt, wt)
		}
		me.appendFmt(true, "}")
	}
	for conv, _ := range me.declConvs {
		snConv = me.safeName(conv)
		me.appendFmt(false, "//\tA convenience interface that declares a type conversion to %v.", conv)
		me.appendFmt(true, "type To%v interface { To%v () %v }", snConv, snConv, conv)
	}

	initLines = append(initLines, "import (")
	for impName, impPath := range me.imports {
		if me.impsUsed[impName] {
			if len(impPath) > 0 {
				initLines = append(initLines, sfmt("\t%s \"%s\"", impName, impPath))
			} else {
				initLines = append(initLines, sfmt("\t\"%s\"", impName))
			}
		}
	}
	initLines = append(initLines, ")", "")
	return strings.Join(append(initLines, me.lines...), "\n")
}

func (me *PkgBag) checkType(typeSpec string) {
	var dt *declType
	tn := ustr.Replace(typeSpec, typeRenderRepls)
	if dt = me.declTypes[tn]; (dt != nil) && (len(dt.EquivalentTo) > 0) {
		tn = dt.EquivalentTo
		dt = me.declTypes[tn]
	}
	if pos := strings.Index(tn, "."); pos > 0 {
		me.impsUsed[tn[:pos]] = true
	}
	if dt != nil {
		dt.render(me)
	} // else if (tn != "string") && (tn != "bool") && (len(tn) > 0) && !strings.Contains(tn, ".") { println("TYPE NOT FOUND: " + tn) }
}

func (me *PkgBag) isParseType(typeRef string) bool {
	for pt, _ := range me.parseTypes {
		if typeRef == pt {
			return true
		}
	}
	return false
}

func (me *PkgBag) resolveQnameRef(ref, pref string, noUsageRec *string) string {
	var ns = me.Schema.XMLNamespaces[""]
	var impName = ""
	if len(ref) == 0 {
		return ""
	}
	if pos := strings.Index(ref, ":"); pos > 0 {
		impName, ns = ref[:pos], me.Schema.XMLNamespaces[ref[:pos]]
		impName = safeIdentifier(impName)
		ref = ref[(pos + 1):]
	}
	if ns == xsdNamespaceUri {
		impName, pref = me.impName, ""
	}
	if ns == me.Schema.TargetNamespace.String() {
		impName = ""
	}
	if noUsageRec == nil { /*me.impsUsed[impName] = true*/
	} else {
		*noUsageRec = impName
	}
	return ustr.PrefixWithSep(impName, ".", me.safeName(ustr.PrependIf(ref, pref)))
}

func (me *PkgBag) rewriteTypeSpec(typeSpec string) (tn string) {
	tn = ustr.Replace(typeSpec, typeRenderRepls)
	if dt := me.declTypes[tn]; (dt != nil) && (len(dt.EquivalentTo) > 0) {
		tn = strings.Replace(typeSpec, tn, dt.EquivalentTo, -1)
	} else {
		tn = typeSpec
	}
	return
}

func (me *PkgBag) safeName(name string) string {
	return ustr.SafeIdentifier(name)
}

func (me *PkgBag) xsdStringTypeRef() string {
	return ustr.PrefixWithSep(me.Schema.XSDNamespacePrefix, ":", "string")
}

type declEmbed struct {
	Name          string
	Annotations   []*Annotation
	elem          element
	finalTypeName string
}

func (me *declEmbed) render(bag *PkgBag, dt *declType) {
	if n := bag.rewriteTypeSpec(me.Name); !dt.memberWritten["E_"+n] {
		dt.memberWritten["E_"+n] = true
		for _, ann := range me.Annotations {
			if ann != nil {
				ann.makePkg(bag)
			}
		}
		me.finalTypeName = bag.rewriteTypeSpec(n)
		bag.appendFmt(true, "\t%s", me.finalTypeName)
	}
}

type declField struct {
	Name, Type, XmlTag string
	Annotations        []*Annotation
	elem               element
	finalTypeName      string
}

func (me *declField) render(bag *PkgBag, dt *declType) {
	for _, ann := range me.Annotations {
		if ann != nil {
			ann.makePkg(bag)
		}
	}
	me.finalTypeName = bag.rewriteTypeSpec(me.Type)
	bag.appendFmt(true, "\t%s %s `xml:\"%s\"`", me.Name, me.finalTypeName, me.XmlTag)
}

type declMethod struct {
	Body, Doc, Name, ReceiverType, ReturnType string
	Annotations                               []*Annotation
	elem                                      element
}

func (me *declMethod) render(bag *PkgBag, dt *declType) {
	for _, ann := range me.Annotations {
		if ann != nil {
			ann.makePkg(bag)
		}
	}
	bag.appendFmt(false, "//\t%s", me.Doc)
	rt := bag.rewriteTypeSpec(me.ReturnType)
	bag.appendFmt(true, "func (me %s) %s %s { %s }", bag.rewriteTypeSpec(me.ReceiverType), ustr.Ifs(strings.Contains(me.Name, "("), me.Name, me.Name+" ()"), rt, strings.Replace(me.Body, me.ReturnType, rt, -1))
}

type declType struct {
	EquivalentTo, Name, Type string
	Embeds                   map[string]*declEmbed
	Annotations              []*Annotation
	Fields                   map[string]*declField
	Methods                  map[string]*declMethod
	elem                     element
	memberWritten            map[string]bool
	rendered                 bool
}

func (me *declType) addAnnotations(a ...*Annotation) {
	me.Annotations = append(me.Annotations, a...)
}

func (me *declType) addField(elem element, n, t, x string, a ...*Annotation) (f *declField) {
	f = &declField{elem: elem, Name: n, Type: t, XmlTag: x, Annotations: a}
	me.Fields[n] = f
	return
}

func (me *declType) addEmbed(elem element, name string, a ...*Annotation) (e *declEmbed) {
	e = &declEmbed{elem: elem, Name: name, Annotations: a}
	me.Embeds[name] = e
	return
}

func (me *declType) addMethod(elem element, recType, name, retType, body, doc string, a ...*Annotation) (m *declMethod) {
	m = &declMethod{elem: elem, Body: body, Doc: doc, Name: name, ReceiverType: recType, ReturnType: retType, Annotations: a}
	me.Methods[name] = m
	return
}

func (me *declType) checkForEquivalents(bag *PkgBag) {
	if (len(me.EquivalentTo) == 0) && (strings.HasPrefix(me.Name, "Txsd") || strings.HasPrefix(me.Name, idPrefix)) {
		for _, dt := range bag.declWrittenTypes {
			if (dt != me) && (len(dt.EquivalentTo) == 0) && me.equivalentTo(dt) {
				me.EquivalentTo = dt.Name
			}
		}
	}
}

func (me *declType) equivalentTo(dt *declType) bool {
	var sme, sdt []string
	if me.Type != dt.Type {
		return false
	}
	if len(me.Embeds) != len(dt.Embeds) {
		return false
	}
	if len(me.Fields) != len(dt.Fields) {
		return false
	}
	sme, sdt = []string{}, []string{}
	for e, _ := range me.Embeds {
		sme = append(sme, e)
	}
	for e, _ := range dt.Embeds {
		sdt = append(sdt, e)
	}
	if !uslice.StrEquivalent(sme, sdt) {
		return false
	}
	sme, sdt = []string{}, []string{}
	for _, f := range me.Fields {
		sme = append(sme, f.Name+f.Type+f.XmlTag)
	}
	for _, f := range dt.Fields {
		sdt = append(sdt, f.Name+f.Type+f.XmlTag)
	}
	if !uslice.StrEquivalent(sme, sdt) {
		return false
	}
	sme, sdt = []string{}, []string{}
	for _, m := range me.Methods {
		if m.Name != "Walk" {
			sme = append(sme, m.Name+m.ReturnType+m.Body)
		}
	}
	for _, m := range dt.Methods {
		if m.Name != "Walk" {
			sdt = append(sdt, m.Name+m.ReturnType+m.Body)
		}
	}
	if !uslice.StrEquivalent(sme, sdt) {
		return false
	}
	return true
}

func (me *declType) render(bag *PkgBag) {
	if !me.rendered {
		me.rendered = true
		if me.checkForEquivalents(bag); len(me.EquivalentTo) == 0 {
			var myName = me.Name
			for _, ann := range me.Annotations {
				if ann != nil {
					ann.makePkg(bag)
				}
			}
			for _, e := range me.Embeds {
				bag.checkType(e.Name)
			}
			for _, f := range me.Fields {
				bag.checkType(f.Type)
			}
			for _, m := range me.Methods {
				bag.checkType(m.ReturnType)
			}
			if len(me.Type) > 0 {
				bag.checkType(me.Type)
				bag.appendFmt(true, "type %s %s", myName, me.Type)
			} else {
				bag.appendFmt(false, "type %s struct {", myName)
				for _, f := range me.Fields {
					f.render(bag, me)
				}
				for _, e := range me.Embeds {
					e.render(bag, me)
				}
				bag.appendFmt(true, "}")
				if PkgGen.AddWalkers && !strings.HasPrefix(myName, idPrefix+"HasAtt") {
					errCheck := sfmt("%s.OnWalkError(&err, &WalkErrors, WalkContinueOnError, WalkOnError) { return }", bag.impName)
					fnCall := "\t\tif fn != nil { if err = fn(me, %v); %s }"
					walkBody := sfmt("\n\tif fn := WalkHandlers.%s; me != nil {\n%s\n", myName, sfmt(fnCall, true, errCheck))
					ec, fc := 0, 0
					bag.walkerTypes[myName] = true
					for _, e := range me.Embeds {
						if bag.walkerTypes[e.finalTypeName] {
							ec++
							walkBody += sfmt("\t\tif err = me.%s.Walk(); %s\n", e.finalTypeName, errCheck)
						}
					}
					for _, f := range me.Fields {
						if bag.walkerTypes[strings.Replace(f.finalTypeName, "*", "", -1)] {
							fc++
							walkBody += sfmt("\t\tif err = me.%v.Walk(); %s\n", f.Name, errCheck)
						} else if strings.HasPrefix(f.finalTypeName, "[]") && bag.walkerTypes[ustr.Replace(f.finalTypeName, typeRenderRepls)] {
							walkBody += sfmt("\t\tfor _, x := range me.%s { if err = x.Walk(); %s }\n", f.Name, errCheck)
						}
					}
					walkBody += sfmt("%s\n}\n\treturn\n", sfmt(fnCall, false, errCheck))
					me.addMethod(nil, "*"+myName, "Walk", "(err error)", walkBody, sfmt("If the WalkHandlers.%v function is not nil (ie. was set by outside code), calls it with this %v instance as the single argument. Then calls the Walk() method on %v/%v embed(s) and %v/%v field(s) belonging to this %v instance.", myName, myName, ec, len(me.Embeds), fc, len(me.Fields), myName))
				}
			}
			bag.declWrittenTypes = append(bag.declWrittenTypes, me)
			for _, m := range me.Methods {
				m.render(bag, me)
			}
		}
	}
}
