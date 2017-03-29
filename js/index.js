$(document).ready(function() {
var template = '<div class="form-group">'
                    +'<label for="exampleInputName2"  class="col-sm-4 control-label">{{Name}}</label>'
                    +'<div class="col-sm-8">'
                    +'    <input type="text" class="form-control" id="exampleInputName2" value="{{RFID}}">'
                    +'</div>'
                +'</div>';

    getData();

    function getData() {
        $.ajax({
            dataType: "json",
            url: "/data",
            success: function(data, textStatus, jqXHR) {
            $("#albumList > div").remove();
            $("#saveButton").show();
                console.log(data);
                for (key in data) {
                    console.log(data[key]);
                    var output = Mustache.render(template, data[key]);
                    console.log(output);
                    $("#updateButton").after(output);
                }

            }
        });
    }

    function checkUpdateStatus(updateStatusId, dialog) {
        $.ajax({
            dataType: "json",
            url: "/updateStatus/"+updateStatusId,
            success: function(data, textStatus, jqXHR) {
                console.log(data);
                if (data.UpdatedSucceeded == true) {
                    getData();
                    dialog.close();
                } else {
                    setTimer(updateStatusId, dialog);
                }
            }
        });
    }

    function setTimer(updateStatusId, dialog) {
        window.setTimeout(checkUpdateStatus.bind(null, updateStatusId, dialog), 1000);
    }

    $("#updateButton").click(function(){
        var dialog = new BootstrapDialog({
                    message: 'Datenbank wird aktualisiert...',
                    closable: false,
                    onshow: function(dialogRef){
                        $.ajax({
                            dataType: "json",
                            url: "/update",
                            success: function(data, textStatus, jqXHR) {
                                console.log(data);
                                updateStatusId = data.UpdateId;
                                checkUpdateStatus(updateStatusId, dialog);
                            }
                        });
                    },
        });
        dialog.open();
    });

    $("#saveButton").click(function(){
        albumList = [];
        $("#albumList > div").each(function(index, value) {
            console.log(value);
            album = $(value).find("label").text();
            rfid = $(value).find("div > input").val();
            console.log(rfid + "#"+album);
            item = {"RFID": rfid, "Name": album};
            albumList.push(item);
        });
        console.log(albumList);
        $.ajax({
            dataType: "json",
            url: "/saveData/"+JSON.stringify(albumList),
            success: function(data, textStatus, jqXHR) {
               console.log(data);
              BootstrapDialog.alert('Datenbank gespeichert');
            }
        });
    });

});