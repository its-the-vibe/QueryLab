package web

const pageTemplate = `
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>QueryLab</title>
  <style>
    body { font-family: sans-serif; margin: 2rem; }
    table { border-collapse: collapse; margin-top: 1rem; min-width: 50%; }
    th, td { border: 1px solid #ccc; padding: 0.4rem 0.6rem; text-align: left; }
    .error { color: #b00020; margin-top: 1rem; }
  </style>
</head>
<body>
  <h1>QueryLab</h1>
  <form method="post" action="/execute">
    <label for="query">Available query</label>
    <select id="query" name="query" required>
      {{range .Queries}}
        <option value="{{.}}" {{if eq $.SelectedQuery .}}selected{{end}}>{{.}}</option>
      {{end}}
    </select>
    <button type="submit">Run</button>
  </form>
  <form method="post" action="/schema/execute" style="margin-top: 1rem;">
    <label for="dataset">Dataset</label>
    <select id="dataset" name="dataset" required>
      {{range .SchemaDatasets}}
        <option value="{{.}}" {{if eq $.SelectedDataset .}}selected{{end}}>{{.}}</option>
      {{end}}
    </select>
    <label for="table">Table</label>
    <select id="table" name="table" required></select>
    <button type="submit">Schema</button>
  </form>
  {{if .Error}}<div class="error">{{.Error}}</div>{{end}}
  {{if .Rows}}
    <table>
      <thead><tr>{{range .Columns}}<th>{{.}}</th>{{end}}</tr></thead>
      <tbody>
        {{range .Rows}}
          <tr>{{range .}}<td>{{.}}</td>{{end}}</tr>
        {{end}}
      </tbody>
    </table>
  {{end}}
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
