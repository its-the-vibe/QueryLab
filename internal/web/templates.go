package web

const queriesPageTemplate = `
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Queries · QueryLab</title>
  <style>
    body { font-family: sans-serif; margin: 2rem auto; max-width: 960px; padding: 0 1rem; color: #202938; }
    nav { display: flex; gap: 1rem; border-bottom: 1px solid #d8dee8; padding-bottom: 1rem; }
    nav a { color: #42526e; text-decoration: none; }
    nav a[aria-current="page"] { color: #155eef; font-weight: 700; }
    main { margin-top: 2rem; }
    form, .results { background: #f7f9fc; border: 1px solid #e1e7ef; border-radius: 8px; margin-top: 1rem; padding: 1.25rem; }
    label { display: block; font-weight: 600; margin-bottom: .5rem; }
    select, input, button { font: inherit; padding: .5rem .7rem; }
    button { background: #155eef; border: 0; border-radius: 4px; color: white; cursor: pointer; margin-left: .5rem; }
    table { border-collapse: collapse; margin-top: 1rem; min-width: 50%; }
    th, td { border: 1px solid #d8dee8; padding: .5rem .7rem; text-align: left; }
    th { background: #f1f4f9; }
    .error { color: #b00020; margin-top: 1rem; }
  </style>
</head>
<body>
  <h1>QueryLab</h1>
  <nav aria-label="Main navigation">
    <a href="/queries" aria-current="page">Queries</a>
    <a href="/schema">Schema</a>
  </nav>
  <main>
    <h2>Run a query</h2>
    <p>Choose an available query to view its results. The selection is included in the page URL.</p>
    <form method="get" action="/queries">
      <label for="query">Available query</label>
      <select id="query" name="query" required>
        {{range .Queries}}
          <option value="{{.Name}}" {{if eq $.SelectedQuery .Name}}selected{{end}}>{{.Name}}</option>
        {{end}}
      </select>
      {{range .Queries}}
        {{if .Parameters}}
          <fieldset class="parameter-group" data-query="{{.Name}}" {{if ne $.SelectedQuery .Name}}hidden disabled{{end}}>
            <legend>Optional parameters for {{.Name}}</legend>
            {{range .Parameters}}
              {{if isBoolean .Type}}
                <label for="param_{{.Name}}">{{.Name}} ({{.Type}})</label>
                <select id="param_{{.Name}}" name="param_{{.Name}}">
                  <option value="" {{if eq (index $.SelectedParameters .Name) ""}}selected{{end}}>Not specified</option>
                  <option value="true" {{if eq (index $.SelectedParameters .Name) "true"}}selected{{end}}>True</option>
                  <option value="false" {{if eq (index $.SelectedParameters .Name) "false"}}selected{{end}}>False</option>
                </select>
              {{else}}
                <label for="param_{{.Name}}">{{.Name}} ({{.Type}})</label>
                <input id="param_{{.Name}}" name="param_{{.Name}}" type="{{inputType .Type}}" value="{{index $.SelectedParameters .Name}}">
              {{end}}
            {{end}}
          </fieldset>
        {{end}}
      {{end}}
      <button type="submit">Run query</button>
    </form>
    {{if .Error}}<div class="error">{{.Error}}</div>{{end}}
    {{if .Rows}}
      <section class="results" aria-label="Query results">
        <h3>Query results</h3>
        <table>
          <thead><tr>{{range .Columns}}<th>{{.}}</th>{{end}}</tr></thead>
          <tbody>
            {{range .Rows}}
              <tr>{{range .}}<td>{{.}}</td>{{end}}</tr>
            {{end}}
          </tbody>
        </table>
      </section>
    {{end}}
  </main>
  <script>
    const querySelect = document.getElementById('query');
    const parameterGroups = document.querySelectorAll('.parameter-group');
    function refreshQueryParameters() {
      for (const group of parameterGroups) {
        group.hidden = group.dataset.query !== querySelect.value;
        group.disabled = group.hidden;
      }
    }
    querySelect.addEventListener('change', refreshQueryParameters);
    refreshQueryParameters();
  </script>
</body>
</html>
`

const schemaPageTemplate = `
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Schema · QueryLab</title>
  <style>
    body { font-family: sans-serif; margin: 2rem auto; max-width: 960px; padding: 0 1rem; color: #202938; }
    nav { display: flex; gap: 1rem; border-bottom: 1px solid #d8dee8; padding-bottom: 1rem; }
    nav a { color: #42526e; text-decoration: none; }
    nav a[aria-current="page"] { color: #155eef; font-weight: 700; }
    main { margin-top: 2rem; }
    form, .results { background: #f7f9fc; border: 1px solid #e1e7ef; border-radius: 8px; margin-top: 1rem; padding: 1.25rem; }
    label { display: block; font-weight: 600; margin: .75rem 0 .5rem; }
    select, button { font: inherit; padding: .5rem .7rem; }
    button { background: #155eef; border: 0; border-radius: 4px; color: white; cursor: pointer; margin-top: 1rem; }
    table { border-collapse: collapse; margin-top: 1rem; min-width: 50%; }
    th, td { border: 1px solid #d8dee8; padding: .5rem .7rem; text-align: left; }
    th { background: #f1f4f9; }
    .error { color: #b00020; margin-top: 1rem; }
  </style>
</head>
<body>
  <h1>QueryLab</h1>
  <nav aria-label="Main navigation">
    <a href="/queries">Queries</a>
    <a href="/schema" aria-current="page">Schema</a>
  </nav>
  <main>
    <h2>Inspect a schema</h2>
    <p>Choose an allowed dataset and table to view its schema. The selection is included in the page URL.</p>
    <form method="get" action="/schema">
      <label for="dataset">Dataset</label>
      <select id="dataset" name="dataset" required>
        {{range .SchemaDatasets}}
          <option value="{{.}}" {{if eq $.SelectedDataset .}}selected{{end}}>{{.}}</option>
        {{end}}
      </select>
      <label for="table">Table</label>
      <select id="table" name="table" required></select>
      <button type="submit">Inspect schema</button>
    </form>
    {{if .Error}}<div class="error">{{.Error}}</div>{{end}}
    {{if .Rows}}
      <section class="results" aria-label="Schema results">
        <h3>Schema results</h3>
        <table>
          <thead><tr>{{range .Columns}}<th>{{.}}</th>{{end}}</tr></thead>
          <tbody>
            {{range .Rows}}
              <tr>{{range .}}<td>{{.}}</td>{{end}}</tr>
            {{end}}
          </tbody>
        </table>
      </section>
    {{end}}
  </main>
  <script>
    const schemaTablesByDataset = {{.SchemaTablesJSON}};
    const tableSelect = document.getElementById('table');
    const selectedTable = {{printf "%q" .SelectedTable}};
    function refreshSchemaTables() {
      const dataset = document.getElementById('dataset').value;
      const tables = schemaTablesByDataset[dataset] || [];
      tableSelect.innerHTML = '';
      for (const table of tables) {
        const option = document.createElement('option');
        option.value = table;
        option.textContent = table;
        if (table === selectedTable) {
          option.selected = true;
        }
        tableSelect.appendChild(option);
      }
    }
    document.getElementById('dataset').addEventListener('change', refreshSchemaTables);
    refreshSchemaTables();
  </script>
</body>
</html>
`
