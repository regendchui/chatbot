package survey

import ( // Imports for HTTP server and HTML forms.
	"context"       // DB insert context.
	"encoding/json" // Serialize visibility logic for frontend runtime evaluation.
	"fmt"           // Error formatting and SQL builder.
	"html"          // HTML escape for safe output.
	"log"           // Log server errors.
	"math"
	"net/http" // HTTP server and handlers.
	"os"       // Listen address and config.
	"strconv"
	"strings" // Path and string helpers.

	"whatsapp-bot/common"
	"whatsapp-bot/db"
) // End import.

// StartSurveyHTTPServer starts background HTTP server for /survey/{slug} forms.
func StartSurveyHTTPServer() { // Call from main after DB and config init.
	addr := strings.TrimSpace(os.Getenv("SURVEY_HTTP_ADDR")) // Bind address.
	if addr == "" {                                          // Default listen address.
		addr = ":8080" // Standard dev port.
	}
	mux := http.NewServeMux()                    // Router.
	mux.HandleFunc("/survey/", handleSurveyPath) // Register survey routes.
	go func() {                                  // Run server in background goroutine.
		log.Printf("survey HTTP listening on %s", addr)        // Startup log.
		if err := http.ListenAndServe(addr, mux); err != nil { // Block until error.
			log.Printf("survey HTTP server error: %v", err) // Log fatal listen errors.
		}
	}() // End goroutine.
} // End StartSurveyHTTPServer.

// handleSurveyPath serves GET form or POST submit for one survey slug.
func handleSurveyPath(w http.ResponseWriter, r *http.Request) { // HTTP entry.
	if r.URL.Path == "/survey/" || r.URL.Path == "/survey" { // Reject bare path.
		http.NotFound(w, r) // 404.
		return              // Stop.
	}
	slug := strings.TrimPrefix(r.URL.Path, "/survey/") // Extract slug after prefix.
	slug = strings.Trim(slug, "/")                     // Remove slashes.
	if strings.Contains(slug, "..") {                  // Reject path traversal.
		http.Error(w, "invalid path", http.StatusBadRequest) // 400.
		return                                               // Stop.
	}
	isBL, bl, fu, err := SurveyBySlug(slug) // Resolve slug to survey definition.
	if err != nil {                         // Unknown slug.
		http.Error(w, "unknown survey", http.StatusNotFound) // 404.
		return                                               // Stop.
	}
	switch r.Method { // Dispatch by verb.
	case http.MethodGet: // Show HTML form.
		prefillPhone := phoneFromSurveyQuery(r)
		if isBL { // Baseline survey.
			writeSurveyForm(
				w,
				bl.Title,
				slug,
				BaselineQuestionsWithSystemFields(bl.Questions),
				true,
				strings.TrimSpace(globalSurveyConfig.Project.Name),
				strings.TrimSpace(globalSurveyConfig.Project.Description),
				strings.TrimSpace(globalSurveyConfig.Project.ConsentFormText),
				strings.TrimSpace(globalSurveyConfig.Project.ConsentFormLabel),
				prefillPhone,
			)
			return
		}
		writeSurveyForm(
			w,
			fu.Title,
			slug,
			fu.Questions,
			false,
			strings.TrimSpace(globalSurveyConfig.Project.Name),
			strings.TrimSpace(globalSurveyConfig.Project.Description),
			"",
			"",
			prefillPhone,
		)
	case http.MethodPost: // Accept submission.
		projectName := ""
		consentFormLabel := ""
		if globalSurveyConfig != nil {
			projectName = strings.TrimSpace(globalSurveyConfig.Project.Name)
			consentFormLabel = strings.TrimSpace(globalSurveyConfig.Project.ConsentFormLabel)
		}
		if isBL { // Baseline submit.
			if err := handleSurveySubmit(w, r, true, bl.TableName, bl.SurveyID, BaselineQuestionsWithSystemFields(bl.Questions), bl.Title, projectName, consentFormLabel); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
			}
			return
		}
		if err := handleSurveySubmit(w, r, false, fu.TableName, fu.SurveyID, fu.Questions, fu.Title, projectName, consentFormLabel); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	default: // Unsupported method.
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed) // 405.
	}
} // End handleSurveyPath.

func phoneFromSurveyQuery(r *http.Request) string {
	if r == nil {
		return ""
	}
	return common.DigitsOnly(strings.TrimSpace(r.URL.Query().Get("phone")))
}

// writeSurveyForm renders a minimal HTML page with phone field + JSON questions.
func writeSurveyForm(w http.ResponseWriter, title string, slug string, questions []SurveyQuestion, isBaseline bool, projectName string, projectDescription string, consentFormText string, consentFormLabel string, prefilledPhone string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")   // UTF-8 HTML.
	var b strings.Builder                                        // Build HTML in memory.
	script := buildSurveyFormClientScript(questions, isBaseline) // Build dynamic frontend validation + visibility script.
	phoneDigitsRequired := configuredSurveyPhoneDigits()
	phonePattern := "[0-9]{8,15}"
	phoneMinLen := "8"
	phoneMaxLen := "15"
	if phoneDigitsRequired > 0 {
		phonePattern = fmt.Sprintf("[0-9]{%d}", phoneDigitsRequired)
		phoneMinLen = strconv.Itoa(phoneDigitsRequired)
		phoneMaxLen = strconv.Itoa(phoneDigitsRequired)
	}
	phoneLabel := translatedSurveyPhoneLabel(isBaseline)
	b.WriteString("<!DOCTYPE html><html><head><meta charset=\"utf-8\"><title>")                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   // Doc start.
	b.WriteString(html.EscapeString(title))                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       // Escape title.
	b.WriteString("</title>")
	writeSurveyPageStyles(&b)
	b.WriteString(`</head><body><div class="survey-wrap">`)
	b.WriteString("<h1>")                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         // Heading open.
	b.WriteString(html.EscapeString(title))                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       // Escape heading.
	b.WriteString("</h1>")                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        // Heading close.
	if strings.TrimSpace(projectName) != "" {
		b.WriteString("<p><strong>" + html.EscapeString(SurveyTranslate(SurveyTranslationProjectLabel, "Project")) + ":</strong> ")
		b.WriteString(html.EscapeString(strings.TrimSpace(projectName)))
		b.WriteString("</p>")
	}
	if strings.TrimSpace(projectDescription) != "" {
		b.WriteString("<p><strong>" + html.EscapeString(SurveyTranslate(SurveyTranslationDescriptionLabel, "Description")) + ":</strong> ")
		b.WriteString(html.EscapeString(strings.TrimSpace(projectDescription)))
		b.WriteString("</p>")
	}
	b.WriteString("<div id=\"formHint\" class=\"form-hint\" role=\"alert\" aria-live=\"polite\"></div>") // Inline hint area for missing required fields.
	b.WriteString("<form id=\"surveyForm\" method=\"post\" action=\"/survey/")                           // Form POST back to same slug.
	b.WriteString(html.EscapeString(slug))                                                               // Escape slug in action.
	b.WriteString("\">")                                                                                 // Close form open tag.
	b.WriteString("<p><label>")                                                                          // Phone label.
	b.WriteString(html.EscapeString(phoneLabel))
	b.WriteString("<br><input required pattern=\"")
	b.WriteString(html.EscapeString(phonePattern))
	b.WriteString("\" maxlength=\"")
	b.WriteString(html.EscapeString(phoneMaxLen))
	b.WriteString("\" minlength=\"")
	b.WriteString(html.EscapeString(phoneMinLen))
	b.WriteString("\" inputmode=\"numeric\" name=\"")
	b.WriteString(RespondentPhoneColumn)
	if prefilledPhone != "" {
		b.WriteString(`" value="`)
		b.WriteString(html.EscapeString(prefilledPhone))
	}
	b.WriteString("\"></label></p>")
	if isBaseline {
		label := strings.TrimSpace(consentFormLabel)
		if label == "" {
			label = SurveyTranslate(SurveyTranslationConsentFormLabel, "Consent Form")
		}
		text := strings.TrimSpace(consentFormText)
		if text == "" {
			text = "Please read and agree to the consent form before answering the baseline survey."
		}
		agreeLabel := SurveyTranslate(SurveyTranslationConsentAgreeLabel, "I have read and agree to the consent form.")
		b.WriteString(`<fieldset id="consentFormBlock" data-required="true" data-question-type="consent" data-question-id="consent_form" data-column-name="consent" data-question-label="` + html.EscapeString(label) + `">`)
		b.WriteString(`<legend>` + html.EscapeString(label) + `<span class="required-badge">*</span></legend>`)
		b.WriteString(`<div style="white-space:pre-wrap;border:1px solid #e2e8f0;background:#f8fafc;border-radius:8px;padding:10px;max-height:240px;overflow:auto;">` + html.EscapeString(text) + `</div>`)
		b.WriteString(`<p style="margin-top:10px;"><label><input type="checkbox" id="baseline_consent_checkbox" name="` + ConsentColumn + `" value="agreed" required> ` + html.EscapeString(agreeLabel) + `</label></p>`)
		b.WriteString(`</fieldset>`)
	}
	qs := questions        // Use JSON-defined questions.
	for _, q := range qs { // Each question.
		writeQuestionField(&b, q) // Append HTML control.
	}
	b.WriteString("<p><button type=\"submit\">Submit</button></p>") // Submit button.
	b.WriteString("</form><script>")                                // Include frontend behavior.
	b.WriteString(script)
	b.WriteString("</script></div></body></html>") // Close doc.
	_, _ = w.Write([]byte(b.String()))             // Write response body.
} // End writeSurveyForm.

// writeQuestionField appends one form control based on question type.
func writeQuestionField(b *strings.Builder, q SurveyQuestion) { // Append to HTML builder.
	col := strings.TrimSpace(q.ColumnName) // Column / field name.
	qid := strings.TrimSpace(q.ID)         // Question ID used by conditional logic evaluator.
	label := html.EscapeString(q.Label)    // Safe label text.
	requiredAttr := "false"                // Track required question flag for JS validation.
	if q.Required {
		requiredAttr = "true"
	}
	questionType := html.EscapeString(strings.ToLower(strings.TrimSpace(q.Type)))
	b.WriteString("<fieldset data-required=\"") // Group each question.
	b.WriteString(requiredAttr)
	b.WriteString("\" data-question-type=\"")
	b.WriteString(questionType)
	b.WriteString("\" data-question-id=\"")
	b.WriteString(html.EscapeString(qid))
	b.WriteString("\" data-column-name=\"")
	b.WriteString(html.EscapeString(col))
	b.WriteString("\" data-question-label=\"")
	b.WriteString(label)
	b.WriteString("\"><legend>")
	b.WriteString(label) // Legend text.
	if q.Required {      // Mark required questions visually.
		b.WriteString("<span class=\"required-badge\">*</span>")
	}
	b.WriteString("</legend>")                          // End legend.
	switch strings.ToLower(strings.TrimSpace(q.Type)) { // Branch on question type.
	case "text": // Free text.
		ph := html.EscapeString(q.Placeholder)        // Escape placeholder.
		b.WriteString("<input type=\"text\" name=\"") // Text input.
		b.WriteString(html.EscapeString(col))         // Name is column.
		b.WriteString("\"")                           // Continue attributes.
		if ph != "" {                                 // Placeholder if any.
			b.WriteString(" placeholder=\"") // Placeholder attr.
			b.WriteString(ph)                // Escaped placeholder.
			b.WriteString("\"")              // Close attr.
		}
		b.WriteString(">") // Close input tag.
	case "numeric": // Numeric free input (validated in frontend script before submit).
		ph := html.EscapeString(q.Placeholder)                              // Escape placeholder.
		b.WriteString("<input type=\"text\" inputmode=\"decimal\" name=\"") // Text input to allow negative sign and decimal.
		b.WriteString(html.EscapeString(col))                               // Name is column.
		b.WriteString("\"")                                                 // Continue attributes.
		if ph != "" {                                                       // Placeholder if any.
			b.WriteString(" placeholder=\"") // Placeholder attr.
			b.WriteString(ph)                // Escaped placeholder.
			b.WriteString("\"")              // Close attr.
		}
		b.WriteString(">") // Close input tag.
	case "multiple_choice": // Radio group.
		for _, ch := range q.Choices { // Each choice.
			b.WriteString("<label><input type=\"radio\" name=\"") // Radio input.
			b.WriteString(html.EscapeString(col))                 // Group name.
			b.WriteString("\" value=\"")                          // Value attr.
			b.WriteString(html.EscapeString(ch.Value))            // Stored value.
			b.WriteString("\"")                                   // Close value.
			b.WriteString("> ")                                   // Close opening tag.
			b.WriteString(html.EscapeString(ch.Label))            // Human label.
			b.WriteString("</label><br>")                         // Line break between radios.
		}
	case "multiple_select": // Checkboxes.
		for _, ch := range q.Choices { // Each option.
			b.WriteString("<label><input type=\"checkbox\" name=\"") // Checkbox.
			b.WriteString(html.EscapeString(col))                    // Name repeats for multi.
			b.WriteString("\" value=\"")                             // Value.
			b.WriteString(html.EscapeString(ch.Value))               // Stored value.
			b.WriteString("\"> ")                                    // Close tag.
			b.WriteString(html.EscapeString(ch.Label))               // Label.
			b.WriteString("</label><br>")                            // Break.
		}
	case "date": // Date picker; block manual typing and rely on picker format.
		b.WriteString("<input type=\"date\" name=\"")
		b.WriteString(html.EscapeString(col))
		b.WriteString("\" onkeydown=\"return false;\" onpaste=\"return false;\" ondrop=\"return false;\">")
	case "slider": // Range slider with live value label.
		min := q.SliderStart
		max := q.SliderEnd
		step := q.SliderStep
		if step <= 0 {
			step = 1
		}
		if max < min {
			min, max = max, min
		}
		defaultValue := min
		rangeID := "slider_" + html.EscapeString(col)
		valueID := "slider_value_" + html.EscapeString(col)
		b.WriteString("<input type=\"range\" name=\"")
		b.WriteString(html.EscapeString(col))
		b.WriteString("\" id=\"")
		b.WriteString(rangeID)
		b.WriteString("\" min=\"")
		b.WriteString(formatSurveyNumber(min))
		b.WriteString("\" max=\"")
		b.WriteString(formatSurveyNumber(max))
		b.WriteString("\" step=\"")
		b.WriteString(formatSurveyNumber(step))
		b.WriteString("\" value=\"")
		b.WriteString(formatSurveyNumber(defaultValue))
		b.WriteString("\" oninput=\"document.getElementById('")
		b.WriteString(valueID)
		b.WriteString("').textContent=this.value;\">")
		b.WriteString(" <span id=\"")
		b.WriteString(valueID)
		b.WriteString("\">")
		b.WriteString(formatSurveyNumber(defaultValue))
		b.WriteString("</span>")
	default: // Unknown type treated as text.
		b.WriteString("<input type=\"text\" name=\"") // Fallback input.
		b.WriteString(html.EscapeString(col))         // Column name.
		b.WriteString("\">")                          // Close input.
	}
	b.WriteString("</fieldset>") // Close fieldset.
} // End writeQuestionField.

// buildVisibilityLogicMap extracts conditional rules keyed by question id.
func buildVisibilityLogicMap(questions []SurveyQuestion) map[string]SurveyVisibilityRule {
	out := map[string]SurveyVisibilityRule{}
	for _, q := range questions {
		id := strings.TrimSpace(q.ID)
		if id == "" || !q.VisibilityLogic.Enabled {
			continue
		}
		out[id] = q.VisibilityLogic
	}
	return out
}

// buildSurveyFormClientScript creates frontend logic for visibility + required checks.
func buildSurveyFormClientScript(questions []SurveyQuestion, isBaseline bool) string {
	logicMap := buildVisibilityLogicMap(questions)
	logicJSON := "{}"
	if raw, err := json.Marshal(logicMap); err == nil {
		logicJSON = string(raw)
	}
	requireConsent := "false"
	if isBaseline {
		requireConsent = "true"
	}
	return fmt.Sprintf(`(function(){
const form=document.getElementById('surveyForm');
const hint=document.getElementById('formHint');
const numericPattern=/^-?(?:\d+\.?\d*|\.\d+)$/;
const logicMap=%s;
const requireConsent=%s==='true';
const consentCheckbox=document.getElementById('baseline_consent_checkbox');
if(!form||!hint){return;}

function getQuestionFieldsets(){
  return Array.from(form.querySelectorAll('fieldset[data-question-type]'));
}
function getFieldsetByQuestionId(questionID){
  return getQuestionFieldsets().find(fs => (fs.getAttribute('data-question-id')||'') === String(questionID||''));
}
function isFieldsetVisible(fs){
  return fs.getAttribute('data-visible') !== 'false';
}
function collectQuestionState(questionID){
  const fs=getFieldsetByQuestionId(questionID);
  if(!fs || !isFieldsetVisible(fs)){
    return { answered:false, value:'', values:[], type:'' };
  }
  const type=(fs.getAttribute('data-question-type')||'').toLowerCase();
  if(type==='multiple_select'){
    const values=Array.from(fs.querySelectorAll('input[type="checkbox"]:checked')).map(el => String(el.value||'').trim()).filter(Boolean);
    return { answered:values.length>0, value:values.join(','), values, type };
  }
  if(type==='multiple_choice'){
    const selected=fs.querySelector('input[type="radio"]:checked');
    const value=String((selected && selected.value) || '').trim();
    return { answered:value!=='', value, values:value?[value]:[], type };
  }
  const control=fs.querySelector('input,textarea,select');
  const value=String((control && control.value) || '').trim();
  return { answered:value!=='', value, values:value?[value]:[], type };
}
function evalCondition(cond){
  const comparator=String((cond && cond.comparator) || '').toLowerCase();
  const expected=String((cond && cond.value) || '').trim();
  const state=collectQuestionState(cond && cond.field);
  if(comparator==='is_answered') return state.answered;
  if(comparator==='is_not_answered') return !state.answered;
  if(!state.answered) return false;
  if(comparator==='equals'){
    if(state.type==='multiple_select') return state.values.includes(expected);
    return state.value===expected;
  }
  if(comparator==='not_equals'){
    if(state.type==='multiple_select') return !state.values.includes(expected);
    return state.value!==expected;
  }
  if(comparator==='contains'){
    if(state.type==='multiple_select') return state.values.includes(expected);
    return state.value.toLowerCase().includes(expected.toLowerCase());
  }
  if(comparator==='not_contains'){
    if(state.type==='multiple_select') return !state.values.includes(expected);
    return !state.value.toLowerCase().includes(expected.toLowerCase());
  }
  if(comparator==='greater_than' || comparator==='less_than'){
    const left=Number(state.value);
    const right=Number(expected);
    if(Number.isNaN(left) || Number.isNaN(right)) return false;
    return comparator==='greater_than' ? left > right : left < right;
  }
  return false;
}
function evaluateVisibilityForFieldset(fs){
  const questionID=fs.getAttribute('data-question-id') || '';
  const rule=logicMap[questionID];
  if(!rule || !rule.enabled) return true;
  if(!Array.isArray(rule.groups) || rule.groups.length===0) return true;
  const groupConnector=String(rule.group_connector || 'or').toLowerCase()==='and' ? 'and' : 'or';
  const groupResults=rule.groups.map(group => {
    const rowConnector=String((group && group.row_connector) || 'and').toLowerCase()==='or' ? 'or' : 'and';
    const conditions=Array.isArray(group && group.conditions) ? group.conditions : [];
    if(conditions.length===0) return true;
    const results=conditions.map(evalCondition);
    return rowConnector==='and' ? results.every(Boolean) : results.some(Boolean);
  });
  return groupConnector==='and' ? groupResults.every(Boolean) : groupResults.some(Boolean);
}
function setFieldsetVisibility(fs, visible){
  fs.setAttribute('data-visible', visible ? 'true' : 'false');
  fs.style.display = visible ? '' : 'none';
  const controls=Array.from(fs.querySelectorAll('input,textarea,select'));
  controls.forEach(control => {
    if(!visible){
      if(control.type==='checkbox' || control.type==='radio'){
        control.checked=false;
      }else{
        control.value='';
      }
      control.disabled=true;
      return;
    }
    control.disabled=false;
  });
}
function applyConditionalVisibility(){
  const fieldsets=getQuestionFieldsets();
  fieldsets.forEach(fs => setFieldsetVisibility(fs, true));
  for(let pass=0; pass<4; pass+=1){
    fieldsets.forEach(fs => {
      const visible=evaluateVisibilityForFieldset(fs);
      setFieldsetVisibility(fs, visible);
    });
  }
}
function validateVisibleRequiredAndNumeric(){
  const missing=[];
  const invalidNumeric=[];
  const fieldsets=getQuestionFieldsets();
  fieldsets.forEach(fs => {
    fs.classList.remove('missing-required');
    if(!isFieldsetVisible(fs)) return;
    const label=fs.getAttribute('data-question-label') || 'Question';
    const type=(fs.getAttribute('data-question-type') || '').toLowerCase();
    const isRequired=fs.getAttribute('data-required')==='true';
    if(type==='multiple_select'){
      if(isRequired && !fs.querySelector('input[type="checkbox"]:checked')){
        missing.push(label);
        fs.classList.add('missing-required');
      }
      return;
    }
    if(type==='multiple_choice'){
      if(isRequired && !fs.querySelector('input[type="radio"]:checked')){
        missing.push(label);
        fs.classList.add('missing-required');
      }
      return;
    }
    const control=fs.querySelector('input,textarea,select');
    const value=String((control && control.value) || '').trim();
    if(isRequired && value===''){
      missing.push(label);
      fs.classList.add('missing-required');
      return;
    }
    if(type==='numeric' && value!=='' && !numericPattern.test(value)){
      invalidNumeric.push(label);
      fs.classList.add('missing-required');
    }
  });
  return { missing, invalidNumeric };
}
function showValidationHint(missing, invalidNumeric){
  if(missing.length===0 && invalidNumeric.length===0){
    hint.style.display='none';
    hint.textContent='';
    return;
  }
  const parts=[];
  if(missing.length>0){
    parts.push('<strong>Please fill in all required fields:</strong><ul>'+missing.map(item => '<li>'+item+'</li>').join('')+'</ul>');
  }
  if(invalidNumeric.length>0){
    parts.push('<strong>Please enter valid numeric values (integer, decimal, or negative):</strong><ul>'+invalidNumeric.map(item => '<li>'+item+'</li>').join('')+'</ul>');
  }
  hint.style.display='block';
  hint.innerHTML=parts.join('');
  window.scrollTo({top:0,behavior:'smooth'});
}
function setBaselineConsentGateState(){
  if(!requireConsent){return true;}
  if(!consentCheckbox){return false;}
  const consented=!!consentCheckbox.checked;
  const controls=Array.from(form.querySelectorAll('input,textarea,select,button'));
  controls.forEach(el => {
    if(el===consentCheckbox){return;}
    el.disabled=!consented;
  });
  return consented;
}

form.addEventListener('input', applyConditionalVisibility);
form.addEventListener('change', applyConditionalVisibility);
form.addEventListener('change', setBaselineConsentGateState);
form.addEventListener('submit', function(e){
  const consented=setBaselineConsentGateState();
  if(requireConsent && !consented){
    e.preventDefault();
    showValidationHint(['Consent form agreement'], []);
    return;
  }
  applyConditionalVisibility();
  const result=validateVisibleRequiredAndNumeric();
  if(result.missing.length>0 || result.invalidNumeric.length>0){
    e.preventDefault();
    showValidationHint(result.missing, result.invalidNumeric);
    return;
  }
  hint.style.display='none';
  hint.textContent='';
});

applyConditionalVisibility();
setBaselineConsentGateState();
})();`, logicJSON, requireConsent)
}

// surveyPageStylesCSS is shared between the survey form and thank-you pages.
const surveyPageStylesCSS = `body{font-family:Inter,-apple-system,BlinkMacSystemFont,"Segoe UI",Arial,sans-serif;line-height:1.5;background:#f8fafc;color:#0f172a;margin:0;padding:18px;}
.survey-wrap{max-width:760px;margin:0 auto;background:#fff;border:1px solid #e2e8f0;border-radius:12px;box-shadow:0 1px 2px rgba(15,23,42,.06);padding:18px;}
h1{margin:0 0 10px 0;font-size:1.35rem;}
h2{margin:18px 0 10px 0;font-size:1.05rem;color:#334155;}
p{margin:8px 0;}
fieldset{margin:14px 0;padding:12px;border:1px solid #d9e2ec;border-radius:10px;background:#fcfdff;}
legend{font-weight:600;padding:0 6px;}
label{display:inline-block;}
input,textarea,select{border:1px solid #cbd5e1;border-radius:8px;padding:8px 10px;font-size:14px;box-sizing:border-box;}
input[type='text'],input[type='number'],textarea,select{width:min(100%,420px);}
input[type='radio'],input[type='checkbox']{width:auto;margin-right:6px;padding:0;}
button{border:1px solid #0f5fd8;background:#0f5fd8;color:#fff;border-radius:8px;padding:8px 14px;font-weight:600;cursor:pointer;}
button:hover{background:#0b4eb6;}
.required-badge{color:#b91c1c;margin-left:6px;}
.missing-required{border-color:#b91c1c;background:#fff5f5;}
.form-hint{display:none;background:#fef2f2;border:1px solid #fecaca;color:#991b1b;padding:10px;border-radius:8px;margin-bottom:12px;}
.form-hint ul{margin:8px 0 0 20px;padding:0;}
.thankyou-msg{font-size:1.05rem;color:#0f172a;margin:12px 0 18px;padding:12px 14px;background:#f0f9ff;border:1px solid #bae6fd;border-radius:10px;}
.thankyou-footer{color:#64748b;margin-top:20px;font-size:0.95rem;}
.summary-list{margin:0;padding:0;list-style:none;}
.summary-item{margin:10px 0;padding:12px 14px;border:1px solid #d9e2ec;border-radius:10px;background:#fcfdff;}
.summary-item dt{font-weight:600;color:#334155;margin:0 0 6px 0;font-size:0.92rem;}
.summary-item dd{margin:0;color:#0f172a;white-space:pre-wrap;word-break:break-word;}
.summary-empty{color:#94a3b8;font-style:italic;}`

func writeSurveyPageStyles(b *strings.Builder) {
	b.WriteString("<style>")
	b.WriteString(surveyPageStylesCSS)
	b.WriteString("</style>")
}

const defaultSurveyThankYouMessage = "Thank you for your response"

func surveyThankYouMessage() string {
	msg := strings.TrimSpace(db.GetProjectSettingString("THANKYOU_MESSAGE", defaultSurveyThankYouMessage))
	if msg == "" {
		return defaultSurveyThankYouMessage
	}
	return msg
}

type surveySummaryRow struct {
	Label string
	Value string
}

func buildResponseSummary(isBaseline bool, questions []SurveyQuestion, values map[string]interface{}, consentFormLabel string) []surveySummaryRow {
	rows := make([]surveySummaryRow, 0, len(questions)+2)
	if raw, ok := values[RespondentPhoneColumn]; ok {
		if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
			rows = append(rows, surveySummaryRow{
				Label: translatedSurveyPhoneLabel(isBaseline),
				Value: s,
			})
		}
	}
	if isBaseline {
		if raw, ok := values[ConsentColumn]; ok {
			if s, ok := raw.(string); ok && strings.TrimSpace(s) == "agreed" {
				label := strings.TrimSpace(consentFormLabel)
				if label == "" {
					label = SurveyTranslate(SurveyTranslationConsentFormLabel, "Consent Form")
				}
				rows = append(rows, surveySummaryRow{
					Label: label,
					Value: SurveyTranslate(SurveyTranslationConsentRecorded, "Agreed"),
				})
			}
		}
	}
	for _, q := range questions {
		cn := strings.TrimSpace(q.ColumnName)
		if cn == "" || cn == RespondentPhoneColumn || (isBaseline && cn == ConsentColumn) {
			continue
		}
		raw := ""
		if v, ok := values[cn]; ok && v != nil {
			if s, ok := v.(string); ok {
				raw = s
			}
		}
		label := strings.TrimSpace(q.Label)
		if label == "" {
			label = cn
		}
		display := formatSurveyAnswerDisplay(q, raw)
		if display == "" {
			display = "—"
		}
		rows = append(rows, surveySummaryRow{Label: label, Value: display})
	}
	return rows
}

func formatSurveyAnswerDisplay(q SurveyQuestion, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(q.Type)) {
	case "multiple_choice":
		if lbl := surveyChoiceLabelByValue(q.Choices, raw); lbl != "" {
			return lbl
		}
		return raw
	case "multiple_select":
		parts := strings.Split(raw, ",")
		labels := make([]string, 0, len(parts))
		for _, p := range parts {
			v := strings.TrimSpace(p)
			if v == "" {
				continue
			}
			if lbl := surveyChoiceLabelByValue(q.Choices, v); lbl != "" {
				labels = append(labels, lbl)
			} else {
				labels = append(labels, v)
			}
		}
		return strings.Join(labels, ", ")
	default:
		return raw
	}
}

func surveyChoiceLabelByValue(choices []SurveyChoice, value string) string {
	for _, ch := range choices {
		if strings.TrimSpace(ch.Value) == value {
			if lbl := strings.TrimSpace(ch.Label); lbl != "" {
				return lbl
			}
			return value
		}
	}
	return ""
}

func writeSurveyThankYouPage(w http.ResponseWriter, surveyTitle string, projectName string, thankYouMsg string, summary []surveySummaryRow) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var b strings.Builder
	b.WriteString("<!DOCTYPE html><html><head><meta charset=\"utf-8\"><title>")
	b.WriteString(html.EscapeString(surveyTitle))
	b.WriteString("</title>")
	writeSurveyPageStyles(&b)
	b.WriteString(`</head><body><div class="survey-wrap">`)
	b.WriteString("<h1>")
	b.WriteString(html.EscapeString(surveyTitle))
	b.WriteString("</h1>")
	if strings.TrimSpace(projectName) != "" {
		b.WriteString("<p><strong>")
		b.WriteString(html.EscapeString(SurveyTranslate(SurveyTranslationProjectLabel, "Project")))
		b.WriteString(":</strong> ")
		b.WriteString(html.EscapeString(strings.TrimSpace(projectName)))
		b.WriteString("</p>")
	}
	b.WriteString(`<p class="thankyou-msg">`)
	b.WriteString(html.EscapeString(strings.TrimSpace(thankYouMsg)))
	b.WriteString(`</p>`)
	b.WriteString("<h2>")
	b.WriteString(html.EscapeString(SurveyTranslate(SurveyTranslationResponseSummary, "Your responses")))
	b.WriteString("</h2>")
	b.WriteString(`<dl class="summary-list">`)
	for _, row := range summary {
		b.WriteString(`<div class="summary-item"><dt>`)
		b.WriteString(html.EscapeString(row.Label))
		b.WriteString(`</dt><dd>`)
		val := strings.TrimSpace(row.Value)
		if val == "" || val == "—" {
			b.WriteString(`<span class="summary-empty">`)
			b.WriteString(html.EscapeString("—"))
			b.WriteString(`</span>`)
		} else {
			b.WriteString(html.EscapeString(val))
		}
		b.WriteString(`</dd></div>`)
	}
	b.WriteString(`</dl>`)
	b.WriteString(`<p class="thankyou-footer">`)
	b.WriteString(html.EscapeString(SurveyTranslate(SurveyTranslationReturnToWhatsApp, "You may return to WhatsApp.")))
	b.WriteString(`</p>`)
	b.WriteString(`</div></body></html>`)
	_, _ = w.Write([]byte(b.String()))
}

// handleSurveySubmit parses POST, validates, INSERTs row, updates meta completion flags.
func handleSurveySubmit(w http.ResponseWriter, r *http.Request, isBaseline bool, tableName string, surveyID string, questions []SurveyQuestion, surveyTitle string, projectName string, consentFormLabel string) error {
	if err := r.ParseForm(); err != nil { // Parse body.
		return fmt.Errorf("parse form: %w", err) // Wrap error.
	}
	phoneDigits, err := normalizeRespondentPhoneForSurvey(r.FormValue(RespondentPhoneColumn))
	if err != nil {
		return err
	}
	if err := validateSQLIdentifier(tableName, "survey table"); err != nil { // Safety.
		return err // Propagate.
	}
	values := map[string]interface{}{}          // column -> validated value.
	values[RespondentPhoneColumn] = phoneDigits // Full international digit string in DB.
	if isBaseline {
		consentValue := strings.TrimSpace(r.FormValue(ConsentColumn))
		if consentValue != "agreed" {
			return fmt.Errorf("you must read and agree to the consent form before starting baseline")
		}
		values[ConsentColumn] = consentValue
	}
	for _, q := range questions { // Each configured question.
		cn := strings.TrimSpace(q.ColumnName)                                // Column name.
		if err := validateSQLIdentifier(cn, "question column"); err != nil { // Safety.
			return err // Stop.
		}
		switch strings.ToLower(strings.TrimSpace(q.Type)) { // By type.
		case "text": // Single string.
			v := strings.TrimSpace(r.FormValue(cn)) // Raw value.
			values[cn] = v                          // Store.
		case "numeric": // Numeric answer; backend stores submitted text as-is.
			v := strings.TrimSpace(r.FormValue(cn)) // Raw user input.
			values[cn] = v                          // Frontend handles numeric format checks before submit.
		case "multiple_choice": // One selected.
			v := strings.TrimSpace(r.FormValue(cn)) // Radio value.
			values[cn] = v                          // Store.
		case "multiple_select": // Many checkboxes same name.
			vs := r.Form[cn]                   // All selected values.
			values[cn] = strings.Join(vs, ",") // CSV string in DB.
		case "date": // Date picker value.
			v := strings.TrimSpace(r.FormValue(cn))
			values[cn] = v
		case "slider": // Slider value.
			v := strings.TrimSpace(r.FormValue(cn))
			values[cn] = v
		default: // Fallback as text.
			v := strings.TrimSpace(r.FormValue(cn)) // Raw.
			values[cn] = v                          // Store.
		}
	}
	cols := []string{RespondentPhoneColumn} // Column order: phone first.
	vals := []interface{}{phoneDigits}      // Parallel values.
	if isBaseline {
		cols = append(cols, ConsentColumn)
		vals = append(vals, values[ConsentColumn])
	}
	for _, q := range questions { // Append remaining columns in JSON order.
		cn := strings.TrimSpace(q.ColumnName) // Column.
		cols = append(cols, cn)               // Add column.
		vals = append(vals, values[cn])       // Add stored string (may be empty for optional).
	}
	placeholders := make([]string, len(vals)) // $1 $2 ...
	for i := range vals {                     // Build placeholders.
		placeholders[i] = fmt.Sprintf("$%d", i+1) // 1-based.
	}
	stmt := fmt.Sprintf( // INSERT statement.
		"INSERT INTO %s (%s) VALUES (%s)",
		tableName,
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "),
	)
	_, err = db.DB.Exec(context.Background(), stmt, vals...) // Execute insert.
	if err != nil {                                          // DB error.
		return fmt.Errorf("save response: %w", err) // Wrap.
	}
	if isBaseline {
		intervalValue := ""
		if raw, ok := values[MessageIntervalColumn]; ok && raw != nil {
			if s, ok := raw.(string); ok {
				intervalValue = strings.TrimSpace(s)
			}
		}
		participantName := ""
		if raw, ok := values[ParticipantNameColumn]; ok && raw != nil {
			if s, ok := raw.(string); ok {
				participantName = strings.TrimSpace(s)
			}
		}
		n, err := db.MarkParticipantBaselineCompleteWithProfileForPhoneDigits(phoneDigits, intervalValue, participantName)
		if err != nil {
			log.Printf("survey submit: baseline meta update error: %v", err)
		} else if n > 1 {
			log.Printf("survey submit: baseline marked complete on %d meta rows (duplicate phone rows merged)", n)
		}
		if schedulingHooks.ScheduleAutoAIMessages != nil {
			if err := schedulingHooks.ScheduleAutoAIMessages(phoneDigits); err != nil {
				log.Printf("survey submit: auto AI message schedule error: %v", err)
			}
		}
		if schedulingHooks.ScheduleAutoFollowupMessages != nil {
			if err := schedulingHooks.ScheduleAutoFollowupMessages(phoneDigits); err != nil {
				log.Printf("survey submit: auto follow-up message schedule error: %v", err)
			}
		}
		if n > 0 && schedulingHooks.AfterBaselineCompleted != nil {
			if err := schedulingHooks.AfterBaselineCompleted(phoneDigits); err != nil {
				log.Printf("survey submit: post-baseline hook error: %v", err)
			}
		}
	} else {
		n, err := db.MarkFollowupCompleteForPhoneDigits(phoneDigits, surveyID)
		if err != nil {
			log.Printf("survey submit: followup meta update error: %v", err)
		} else if n > 1 {
			log.Printf("survey submit: followup marked complete on %d meta rows", n)
		}
		if schedulingHooks.DeletePendingFollowup != nil {
			if err := schedulingHooks.DeletePendingFollowup(phoneDigits, surveyID); err != nil {
				log.Printf("survey submit: remove pending follow-up prompts error: %v", err)
			}
		}
	}
	summary := buildResponseSummary(isBaseline, questions, values, consentFormLabel)
	writeSurveyThankYouPage(w, surveyTitle, projectName, surveyThankYouMessage(), summary)
	return nil
} // End handleSurveySubmit.

// normalizeRespondentPhoneForSurvey strips non-digits and enforces 8-15 digit full international numbers.
func normalizeRespondentPhoneForSurvey(raw string) (string, error) {
	d := common.DigitsOnly(strings.TrimSpace(raw))
	exactDigits := configuredSurveyPhoneDigits()
	if exactDigits > 0 && len(d) != exactDigits {
		return "", fmt.Errorf("phone must be exactly %d digits including country code (digits only)", exactDigits)
	}
	if len(d) < 8 || len(d) > 15 {
		return "", fmt.Errorf("phone must be 8-15 digits including country code (digits only)")
	}
	return d, nil
}

func configuredSurveyPhoneDigits() int {
	n := db.GetProjectSettingInt("SURVEY_PHONE_DIGITS", 0)
	if n < 0 {
		return 0
	}
	return n
}

func formatSurveyNumber(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "0"
	}
	if math.Mod(v, 1) == 0 {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func translatedSurveyPhoneLabel(isBaseline bool) string {
	const fallback = "Your WhatsApp number (digits only, include country code, e.g. 85254036581)"
	if isBaseline {
		if v := SurveyTranslate(SurveyTranslationBaselinePhoneLabel, ""); strings.TrimSpace(v) != "" {
			return v
		}
	}
	if !isBaseline {
		if v := SurveyTranslate(SurveyTranslationFollowupPhoneLabel, ""); strings.TrimSpace(v) != "" {
			return v
		}
	}
	return SurveyTranslate(SurveyTranslationRespondentPhoneLabel, fallback)
}
