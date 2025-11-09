import ApexCharts from "apexcharts";

// ===== chartSeven
const chart13 = () => {
  const chartThirteenOptions = {
    series: [900, 700, 850],
    colors: ["#3641f5", "#7592ff", "#dde9ff"],
    labels: ["Affiliate", "Direct", "Adsense"],
    chart: {
      fontFamily: "Outfit, sans-serif",
      type: "donut",
      width: 280,
      height: 280,
    },
    stroke: {
      show: false,
      width: 4, // Creates a gap between the series
      colors: "transparent", // Gap color (use background color to make it seamless)
    },
    plotOptions: {
      pie: {
        donut: {
          size: "65%",
          background: "transparent",
          labels: {
            show: true,
            name: {
              show: true,
              offsetY: 0,
              color: "#1D2939",
              fontSize: "12px",
              fontWeight: "normal",
              text: "Total 3.5K",
            },
            value: {
              show: true,
              offsetY: 10,
              color: "#667085",
              fontSize: "14px",
              formatter: () => "Used of 1.1K",
            },
            total: {
              show: true,
              label: "Total",
              color: "#000000",
              fontSize: "20px",
              fontWeight: "bold",
            },
          },
        },
      },
    },
    dataLabels: {
      enabled: false,
    },

    tooltip: {
      enabled: false,
    },

    legend: {
      show: false,
    },

    responsive: [
      {
        breakpoint: 640,
        options: {
          chart: {
            width: 280,
            height: 280,
          },
        },
      },
      {
        breakpoint: 2600,
        options: {
          chart: {
            width: 240,
            width: 240,
          },
        },
      },
    ],
  };

  const chartSelector = document.querySelectorAll("#chartThirteen");

  if (chartSelector.length) {
    const chartThirteen = new ApexCharts(
      document.querySelector("#chartThirteen"),
      chartThirteenOptions,
    );
    chartThirteen.render();
  }
};

export default chart13;
