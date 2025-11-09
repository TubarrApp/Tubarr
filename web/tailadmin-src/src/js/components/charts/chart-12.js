import ApexCharts from "apexcharts";

// ===== chartTwo
const chart12 = () => {
  const chartTwelveOptions = {
    series: [90],
    colors: ["#465FFF"],
    chart: {
      fontFamily: "Outfit, sans-serif",
      type: "radialBar",
      height: 360,
      sparkline: {
        enabled: true,
      },
    },
    plotOptions: {
      radialBar: {
        startAngle: -85,
        endAngle: 85,
        hollow: {
          size: "80%",
        },
        track: {
          background: "#E4E7EC",
          strokeWidth: "100%",
          margin: 5, // margin is in pixels
        },
        dataLabels: {
          name: {
            show: false,
          },
          value: {
            fontSize: "36px",
            fontWeight: "600",
            offsetY: -25,
            color: "#1D2939",
            formatter: function (val) {
              return "$" + val;
            },
          },
        },
      },
    },
    fill: {
      type: "solid",
      colors: ["#465FFF"],
    },
    stroke: {
      lineCap: "round",
    },
    labels: ["June Goals"],
  };

  const chartSelector = document.querySelectorAll("#chartTwelve");

  if (chartSelector.length) {
    const chartTwelve = new ApexCharts(
      document.querySelector("#chartTwelve"),
      chartTwelveOptions,
    );
    chartTwelve.render();
  }
};

export default chart12;
